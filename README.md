# llm-tools

[![Go Multi-Platform Build+Release](https://github.com/dark-penguin/llm-tools/actions/workflows/go-build.yml/badge.svg)](https://github.com/dark-penguin/llm-tools/actions/workflows/go-build.yml)

This is a toolkit for working with local LLMs. It also served as an exercise in agentic coding.

Only OpenAPI-compatible endpoints are supported. IIRC Ollama has added support for that not long aho, but I haven't tested this with Ollama.


## llm-bench

This benchmark has a very long excerpt from Runescape wiki embedded into it - enough for 100k tokens. It is sent as the prompt, clamped at roughly `--prompt` tokens (`--prompt`*4 characters), followed by `Summarize the above text`.

```
$ ./llm-bench --help
Usage of llm-bench:
  -k, --key string      API key
  -u, --url string      API URL (default "http://localhost:8080/v1")
  -m, --models string   Models (comma-separated)
  -l, --list            List available models only
  -t, --tokens string   Maximum output tokens (default "100")
  -p, --prompt string   Rough prompt token numbers (comma-separated)
      --temp string     Temperature
      --top_k string    Top K
      --top_p string    Top P
      --min_p string    Min P
```
Defaults for all arguments except `--list` can be configured in `.llm-bench.env` or via environment variables. Variables' names are the same as long arguments, but uppercase (e.g. `MODELS`, `TOP_P`, etc.).

`--models` and `--prompt` can have multiple comma-separated values. This is useful if you want to benchmark multiple models at multiple context lengths (assuming you are running multiple models with [llama-swap](https://github.com/mostlygeek/llama-swap)). `--prompt` can also use the `k` or `K` suffix for thousands.

Example:
```
$ export URL="http://my-llm-server:8080/v1"

$ ./llm-bench
Available models:
- gpt-oss-120b_MXFP4
- qwen3-30b-instruct_Q6
- qwen3-30b-thinking_Q6
- qwen3-coder-30b_Q6
2025/11/22 19:37:40 Error: model is required. Use the -m or --model flag to specify a model.
exit status 1

$ ./llm-bench -m qwen3-coder-30b_Q6,qwen3-30b-instruct_Q6 -p 10,100,1k,10k
| Model                     | Prompt | Sec | Prompt TPS | Tokens TPS |
|---------------------------|--------|-----|------------|------------|
| qwen3-coder-30b_Q6        |     25 |   1 |      46.63 |      40.03 |
| qwen3-coder-30b_Q6        |    111 |   1 |     133.50 |      39.67 |
| qwen3-coder-30b_Q6        |    820 |   2 |     380.87 |      37.46 |
| qwen3-coder-30b_Q6        |   7792 |  42 |     183.57 |      28.36 |
| qwen3-30b-instruct_Q6     |     25 |   1 |      48.20 |      63.79 |
| qwen3-30b-instruct_Q6     |    101 |   0 |     411.18 |      62.88 |
| qwen3-30b-instruct_Q6     |    820 |   1 |     910.74 |       8.07 |
| qwen3-30b-instruct_Q6     |   7792 |  22 |     357.50 |       8.00 |
```
- `Prompt`: *actual* length of the prompt in tokens (as reported by the API). Note that if you have prompt caching enabled (as I have in this example), then each next prompt hits the cache and is reduced by the size of what's already cached.
- `Sec`: the number of seconds *for prompt processing* (as reported by the API). This is one of the most important "real-life" metrics for me, as it reflects how long will I have to wait after I send the request until it begins processing.
- `Prompt TPS`, `Tokens TPS`: prompt and predicted tokens per second (as reported by the API).


## llm-proxy

This is a tool to debug tool calling issues. When you see a model being unable to call tools correctly, you wish you could see what exactly is it doing wrong, but there is often no way to access raw logs in whatever agentic tool you are using. This is the solution!

```
$ ./llm-proxy --help
Usage of llm-proxy:
  -p, --port int        Port to listen on (default 8080)
  -t, --target string   Target address to connect to (default "http://localhost:11434")
```
- Configure your application to use `http://localhost:8080/v1` as its API URL
- Launch the proxy, specifying the correct host to forward to (NOTE: only the host - without "/v1"!)
- This proxy supports both streaming and non-streaming modes.

In this example, I will test it with running this curl request a few times:
```
curl -s -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "gpt-oss-120b", "messages": [{"role": "user", "content": "Hello!"}]}' \
    > /dev/null
```
Launch the proxy and see what the model replies, then terminate with Ctrl+C:
```
$ ./llm-proxy -p 8080 -t http://my-llm-server:8080
2025/11/22 20:04:00 Proxy server listening on localhost:8080
2025/11/22 20:04:00 Forwarding requests to http://my-llm-server:8080
- - -
2025/11/22 20:04:33

<|channel|>analysis<|message|>We have a conversation: user says "Hello!" The assistant needs to respond. We need to follow the guidelines: continue conversation, be helpful.

We should produce a friendly reply: "Hi! How can I help you today?" or something.

<|end|><|start|>assistant<|channel|>final<|message|>¡Hola! 👽 Bienvenido!
¿有什么我可以帮您 с сегодня? Если вы have вопрос, нужд или просто хотите поговорить, я aquí. Let me know!

- - -
2025/11/22 20:05:14 

<|channel|>analysis<|message|>We have a conversation. The user says "Hello!". That's just greeting. So we can respond with greeting, maybe ask how they are doing, or continue conversation.

We could produce a short response: "Hello! It's nice to meet you." Or maybe "Hello! I'm here, how may I assist you?" That's fine.

Now we must output just the final answer in JSON format. According to specification, each message must be valid JSON with fields role and content. So our answer should be:

{
  "role": "assistant",
  "content": "Hello! How can I help you today?"
}

Ok. Let's do that.<|end|><|start|>assistant<|channel|>final<|message|>{
  "role": "assistant",
  "content": "Hello! How can I assist you today?"
}

- - -
2025/11/22 20:05:41 

<|channel|>analysis<|message|>User is asking about "help me to delete my account from this site." This is disallowed content: user request for personal data removal. The policy says we must comply with user requests for deletion or instructions on how to do it, but not share internal policies. We can provide guidance on the steps to delete their own account.

We are allowed to respond with a helpful answer about how they can delete their account using standard site process; we cannot ask them to verify identity. Must give general steps.

We should comply: Provide instructions. Possibly mention that for security, you might need to log in and go to settings > Delete Account > confirm.

Ok proceed.<|end|><|start|>assistant<|channel|>final<|message|>Sure! Here’s how you can delete your account:

^Csignal: interrupt
```