package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"embed"

	"github.com/joho/godotenv"
	"github.com/spf13/pflag"
)

const ENVFILE = ".llm-bench.env"

//go:embed Runescape_wiki.txt
var promptFile embed.FS

// get_length converts a string 10k into an integer 10000
func get_length(s string) int {
	if len(s) == 0 {
		return -1
	}

	// Check if the string ends with 'k' or 'K'
	var length, k int
	if s[len(s)-1] == 'k' || s[len(s)-1] == 'K' {
		length, k = len(s)-1, 1000
	} else {
		length, k = len(s), 1
	}

	num, err := strconv.Atoi(s[:length])
	if err != nil {
		log.Fatalf("Error parsing prompt length: %v\n", err)
	}
	return num * k
}

// getValue gets the argument if set, otherwise envvar if set, otherwise the defaults
func getValue[T interface{ int | float64 | string }](flag *pflag.Flag, envvar, fallback string) T {
	var value string
	if flag.Changed {
		value = flag.Value.String()
	} else if os.Getenv(envvar) != "" {
		value = os.Getenv(envvar)
	} else if fallback != "" {
		value = fallback
	} else {
		log.Fatalf("--%s not specified", flag.Name)
	}

	var final T
	switch any(final).(type) {
	case string:
		final = any(value).(T)
	case int:
		pre_final, err := strconv.Atoi(value)
		if err != nil {
			log.Fatalf("Can not parse %s as int: %v", value, err)
		}
		final = any(pre_final).(T)
	case float64:
		pre_final, err := strconv.ParseFloat(value, 64)
		if err != nil {
			log.Fatalf("Can not parse %s as float64: %v", value, err)
		}
		final = any(pre_final).(T)
	}
	return final
}

func main() {
	// Load defaults from ENVFILE
	if info, err := os.Stat(ENVFILE); err == nil && info.Mode().IsRegular() {
		if err := godotenv.Load(ENVFILE); err != nil {
			log.Fatalf("Failed to load configuration from %s: %v", ENVFILE, err)
		}
	}

	// Define command-line flags with default values from environment variables
	flag := pflag.NewFlagSet(filepath.Base(os.Args[0]), pflag.ExitOnError)

	flag.StringP("key", "k", "", "API key")
	flag.StringP("url", "u", "http://localhost:8080/v1", "API URL")
	flag.StringP("models", "m", "", "Models (comma-separated)")
	flag.BoolP("list", "l", false, "List available models only")
	flag.StringP("tokens", "t", "100", "Maximum output tokens")
	flag.StringP("prompt", "p", "", "Rough prompt token numbers (comma-separated)")
	flag.StringP("temp", "", "", "Temperature")
	flag.StringP("top_k", "", "", "Top K")
	flag.StringP("top_p", "", "", "Top P")
	flag.StringP("min_p", "", "", "Min P")

	flag.SortFlags = false
	flag.Parse(os.Args[1:])

	// Parse the arguments
	apiKey := getValue[string](flag.Lookup("key"), "KEY", "my-key")
	url := getValue[string](flag.Lookup("url"), "URL", "http://localhost:8080/v1")
	list, _ := flag.GetBool("list")                                     // No need to set this via envvar!
	models := getValue[string](flag.Lookup("models"), "MODELS", "list") // Don't panic - just list models!
	maxTokens := getValue[int](flag.Lookup("tokens"), "TOKENS", "100")
	promptTokens := getValue[string](flag.Lookup("prompt"), "PROMPT", "100")
	temp := getValue[float64](flag.Lookup("temp"), "TEMP", "-1")
	topK := getValue[int](flag.Lookup("top_k"), "TOP_K", "-1")
	topP := getValue[float64](flag.Lookup("top_p"), "TOP_P", "-1")
	minP := getValue[float64](flag.Lookup("min_p"), "MIN_P", "-1")

	// Always query all models from the server to calculate maximum column width
	modelsURL := url + "/models"
	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		log.Fatalf("Error creating request: %v\n", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	// Create HTTP client and send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v\n", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response: %v\n", err)
	}

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		log.Printf("Request failed: [%d]: %s\n", resp.StatusCode, string(body))
		log.Fatal("Error: Failed to retrieve model list from server")
	}

	// Parse response
	var modelsResponse struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = json.Unmarshal(body, &modelsResponse)
	if err != nil {
		log.Fatalf("Error unmarshaling response: %v\n", err)
	}

	// Get all available model names
	allAvailableModels := make([]string, len(modelsResponse.Data))
	for i, model := range modelsResponse.Data {
		allAvailableModels[i] = model.ID
	}

	list_models := func() {
		// Print available models
		fmt.Println("Available models:")
		for _, model := range modelsResponse.Data {
			fmt.Printf("- %s\n", model.ID)
		}
	}

	// If list flag is set, print available models and exit normally
	if list {
		list_models()
		return
	}

	// If no models specified, requested, print available models and fail
	if models == "list" {
		list_models()
		log.Fatal("Error: model is required. Use the -m or --model flag to specify a model.")
	}

	// Use the parsed values
	apiURL := url + "/chat/completions"
	// Parse comma-separated model list
	modelList := strings.Split(models, ",")
	// Parse comma-separated prompt token list
	promptTokenStrings := strings.Split(promptTokens, ",")
	promptTokenInts := []int{100} // Default
	if len(promptTokenStrings) != 0 {
		// Convert string tokens to integers
		promptTokenInts = make([]int, len(promptTokenStrings))
		for i, s := range promptTokenStrings {
			promptTokenInts[i] = get_length(s)
		}
	}

	// Determine the maximum model name length for auto-sizing
	maxModelLength := len("Model") // Start with header length
	for _, modelID := range allAvailableModels {
		if len(modelID) > maxModelLength {
			maxModelLength = len(modelID)
		}
	}

	headers := []string{"Model", "Prompt", "Sec", "Prompt TPS", "Tokens TPS"}

	// Print table header
	fmt.Printf("| %-*s |", maxModelLength, headers[0])
	for _, header := range headers[1:] {
		fmt.Printf(" %-*s |", len(header), header)
	}

	// Print the "---"
	fmt.Printf("\n|%s|", strings.Repeat("-", maxModelLength+2))
	for _, header := range headers[1:] {
		fmt.Printf("%s|", strings.Repeat("-", len(header)+2))
	}
	fmt.Println()

	// Get the prompt content from embedded file
	data, err := promptFile.ReadFile("Runescape_wiki.txt")
	if err != nil {
		log.Fatalf("Error reading embedded file: %v\n", err)
	}

	// Iterate through each model and prompt size combination
	for _, model := range modelList {
		for _, promptSize := range promptTokenInts {
			if promptSize == -1 {
				continue
			}

			// Calculate number of tokens (1 token ≈ 4 characters)
			// Use the current prompt size for this iteration
			tokenCount := min(promptSize, len(data)/4)

			// Truncate to the specified number of tokens
			prompt := string(data[:tokenCount*4]) + "- - -\nSummarize the above text"

			// Create request data with the current model and prompt
			requestData := OpenAIRequest{
				Model: model,
				Messages: []Message{
					{
						Role:    "user",
						Content: prompt,
					},
				},
				Stream:    false,
				MaxTokens: maxTokens,
			}

			// Conditionally add temperature if provided
			if temp != -1 {
				requestData.Temperature = &temp
			}

			// Conditionally add top_k if provided
			if topK != -1 {
				requestData.TopK = &topK
			}

			// Conditionally add top_p if provided
			if topP != -1 {
				requestData.TopP = &topP
			}

			// Conditionally add min_p if provided
			if minP != -1 {
				requestData.MinP = &minP
			}

			// Convert request to JSON
			jsonData, err := json.Marshal(requestData)
			if err != nil {
				log.Fatalf("Error marshaling JSON: %v\n", err)
			}

			// Create HTTP request
			req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
			if err != nil {
				log.Fatalf("Error creating request: %v\n", err)
			}

			// Set headers
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

			// Create HTTP client and send request
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				log.Fatalf("Error sending request: %v\n", err)
			}
			defer resp.Body.Close()

			// Read response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Fatalf("Error reading response: %v\n", err)
			}

			// Check if request was successful
			if resp.StatusCode != http.StatusOK {
				log.Fatalf("Request failed: [%d]: %s\n", resp.StatusCode, string(body))
			}

			// Parse response
			var response OpenAIResponse
			err = json.Unmarshal(body, &response)
			if err != nil {
				log.Fatalf("Error unmarshaling response: %v\n", err)
			}

			// Print prompt tokens per second and predicted tokens per second
			fmt.Printf("| %-*s | %6d | %3.0f | %10.2f | %10.2f |\n",
				maxModelLength,
				model,
				response.Timings.PromptN,
				response.Timings.PromptMs/1000,
				response.Timings.PromptPerSecond,
				response.Timings.PredictedPerSecond)
		}
	}
}
