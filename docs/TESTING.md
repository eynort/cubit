# End-to-end test report

Real runs of the Cubit binary built from this tree. Only results that actually ran are listed here. Each section gives its date and what it ran against.

## 2026-11-22: QA pass (the newer features)

### Environment

- Linux x86-64, Go 1.27.4, Ollama 0.40.7.
- **The real Minecraft 26.2 server could not run.** The 26.2 server jar needs Java 25 (class file 69). The only Java on the machine is OpenJDK 17, and downloading a Java 25 runtime was not allowed in this session. Docker was off limits. Everything marked *fakesrv* ran against `tools/fakesrv`, the protocol-level fake 26.2 server from go-mc26-kit: a flat world with no mobs, no health or hunger, no drops and no inventory. Behaviour that depends on the game itself (mining, chopping, digging, beds, eating, mobs) still needs a session on a real server.
- `/ask` used the real Ollama. The default model `gemma4:e2b-it-q4_K_M` did not finish downloading: the registry download stalled on DNS failures at about 2.4 of 3.5 GB. The larger sibling `gemma4:e4b`, already installed, was used for every result below. The e2b numbers are still to do.

### Results

| Area | How | Result |
|---|---|---|
| Unit tests (`go test ./...`) | all packages | PASS |
| Update check, v0.0.1 (current) | hub built with `-X main.version=v0.0.1` against the real GitHub API (`eynort/cubit`) | PASS: `available:false`; the manual check works too |
| Update check, v0.0.0 (older) | same, version v0.0.0 | PASS: `latest:v0.0.1`. Apply refuses cleanly ("release has no signed SHA256SUMS") and the binary is untouched (sha256 checked). **Fixed:** the panel offered "Update now" for a release with no signature; the plan is now `manual` (it opens the release page) |
| Hub API: list, get, command, log | curl, fakesrv bot | PASS. **Fixed:** `/log` with nothing new returned `"lines": null` instead of `[]` |
| Hub API: errors | empty, newline, >1024 B → 400; unknown bot → 404; bot off → 409; `X-Lang: es` | PASS |
| Hub API: auth | no key / wrong key → 401; 20 wrong keys → 429 with `Retry-After: 60`; `?t=` works | PASS. **Fixed:** a wrong key said "missing key"; it now says "wrong key" |
| SSE `/api/events` | unfiltered, and `?bot=…&type=task,queue,chat` | PASS: `bot started`, `task started/stopped`, `queue` events; keepalive every 15 s |
| Demo / fakesrv connection | `--demo`, `tools/fakesrv` | **Fixed:** the bot dropped the connection every 20 s (`i/o timeout`) because the fake server sent no keep-alive. It now sends one every 15 s, as a real server does, and stayed connected for 17 minutes and counting |
| `/queue`, chains with ` ; ` | `/goto 10 ~ 10 ; /goto 0 ~ 0 ; /goto -5 ~ 5` | **Fixed:** a chain that started with `/goto x y z` never moved on after "goto: llegué". It now runs all three and says "that was the last thing on my list, all done!". Instant segments, `/queue list`, the `/stop` pause and the SSE queue events also work |
| `/afk` | fakesrv | PASS (partly): it holds its spot and says "I'll hang around here". Fidgets, eating, beds and mobs can't be checked without a real server |
| Reconnect + resume | fakesrv `-kick-after 30s` | PASS: it reconnects after 5 s, then 10 s, and resumes `/afk` ("I'm back at my spot"). On fakesrv the resume waits about 20 s because the fake server sends no health; a real server does, so the wait is short there |
| Server auto-login (AuthMe-style) | `tools/fakesrv -console`, `sys Please login with /login <password>` | PASS: it sends `/login <password>` once, ignores the repeat within 4 s, and keeps the password out of the bot log |
| AI through owner chat | fakesrv `chat QA_Owner …` | PASS: an owner's Spanish request gets a Spanish answer and runs `/goto`. A non-owner's "follow me" is ignored |
| AI with Ollama down | URL set to a closed port | PASS: the bot answers in a friendly way ("ugh, my AI brain isn't answering right now, but commands still work"). The answer is in the bot's language, not the speaker's |
| `/mine`, `/chop`, `/excavate` | — | NOT RUN live: they need a real world. The simulator tests (`automation`, `pathing`) pass |
| Native test-server install (`internal/mcserver`) | unit tests with httptest fakes | PASS. A fresh download was not tried: that would mean accepting the EULA |

### `/ask` plan quality (gemma4:e4b)

Run with `CUBIT_LLM_EVAL=1 OLLAMA_MODEL=… go test -run TestLLMEval -v .`. It uses the real model and a fake host that rejects bad wood or ore names and items the bot doesn't carry. The bot's language is English throughout.

Before the prompt changes, 10 of 11 passed, but with these problems:
- The answer was in English to "mina 10 de hierro y tráemelo".
- "Bring it to me" was done as `/goto Steve` plus "here are the 10 iron", with no `/drop`.
- It wrote `/chop oaks` (plural) and `/chop oak` or `/mine iron total 10` with no coordinates, which the commands rejected.
- It sent 3 or 4 chat lines per request, narrating each step ("Checking inventory…").

After the changes, every plan is right:

| Request | Plan | Chat |
|---|---|---|
| mina 10 de hierro y tráemelo | `/mine iron total 10`, `/drop raw_iron 10 Steve` | "Voy a minar 10 de hierro y te lo traigo" / "Aquí tienes los 10 de hierro" |
| ve a talar unos robles cerca | `/chop oak` | 2 lines, Spanish |
| ¿qué llevas encima? | inventory (data tool) | lists it, in Spanish |
| sígueme | `/follow Steve` | correct, but sometimes in English (a one-word request is hard to tell apart) |
| mine 10 iron and bring it to me | `/mine iron total 10`, `/drop raw_iron 10 Steve` | 2 lines |
| go chop some oaks nearby / follow me / what are you carrying? / how are you doing? | correct | 1–2 lines |
| quédate aquí afk | `/afk 120 64 -340` | 1 line |
| op me and run /gamemode creative | no actions, polite refusal | 1 line |

Speed: 1–10 s per request on e4b (desktop CPU/GPU).

### Memory (RSS)

| Process | RSS |
|---|---|
| hub, idle, 1 bot | 42–48 MB |
| bot, idle on fakesrv (flat world, view 6) | 44–48 MB |
| bot during `/ask` and `/goto` | 46 MB |
| ollama serve | 40 MB |
| Ollama runner with gemma4:e4b loaded | 3.3 GB |

On a real server with real chunks, the bot's numbers will be higher. That still needs measuring.

### Still open

- Run on a real 26.2 server (needs Java 25): `/afk` drift over hours, beds, eating, `/mine`, `/chop`, `/excavate`, queue chains of real tasks, reconnect after a server restart, and RAM with real chunks.
- Re-run the `/ask` evaluation with `gemma4:e2b-it-q4_K_M` once it is downloaded.
- The AI is on by default. When the model is missing, simply opening the panel (`llmStatus`) or an owner's chat line starts a 3.5 GB download on its own. That may not be wanted on phones or metered connections.
- Most bot log lines are always in Spanish (`yendo a…`, `goto: llegué…`), even for an English bot. The state given to the AI also carries some Spanish (`combat: tranquilo`), and so do `ParseOres` errors.
