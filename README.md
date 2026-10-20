# tars

**Your terminal AIs, working together in a group chat.**
Claude, Codex, Antigravity, Cursor, OpenCode and Ollama in the same room: they read the same conversation, tag each other with `@`, hand off work, and tag **you** when they need a decision.

No Electron, no server, no account. One Go binary that drives the AI CLIs you already have installed.

[Português](README.pt-BR.md)

```
 Site da padaria  @lead @dev @design
 disco 41,2 GB livre  ·  Claude 34% 5h · Codex 12% semana

 você @andre  14:02
   landing page nova pra padaria, cores quentes, mobile primeiro

 @lead  14:02
   @design escolhe paleta e fontes. @dev monta a página em Next.js quando ela terminar.

 @design  14:03
   Paleta: #F4A261 casca, #2A1E17 café, #FFF8F0 farinha. Fontes: Fraunces + Inter.

 @dev  14:05
   Página pronta em app/page.tsx com a paleta. Rodei o build e passou.

 @lead  14:05
   @andre a primeira versão está pronta. Fotos dos produtos reais ou ilustração?

 ⠙ @dev trabalhando 0:42
 › Mensagem pro grupo
```

> The interface speaks English, Portuguese, Spanish, French, German, Italian, Japanese and Chinese. It follows your Mac's language (or `language = "pt"` in the config), and the AIs reply in the same language.

## Why

Each AI is good at something different. Instead of copy-pasting between five terminal tabs, you create a group once, give each AI a role, and talk to all of them in one place.

## Features

- **Group setup in 5 steps**: goal (every AI reads it), who joins, each AI's profile (`@handle`, role, model, color, access), the lead, review.
- **@ mentions everywhere**: `@dev do X` wakes only `@dev`; `@all` (or `@todos`) wakes everyone; no mention goes to the lead (or to everyone, if you prefer).
- **AIs tag each other**: whoever delegates is called back automatically with the result. A loop guard stops after 12 AI turns in a row.
- **They tag you**: you get a macOS notification and a badge on the group list.
- **Two access levels per AI**: only the group's folder (safe mode of each CLI) or the whole Mac, which skips every permission prompt *by your explicit choice*. With [cua-driver](https://github.com/trycua/cua) they can also drive the browser and native apps in the background.
- **Images**: drag a file into the terminal or press `ctrl+v` with a screenshot copied.
- **Voice**: `ctrl+r` records and transcribes locally with Whisper. Nothing leaves your Mac. Optional, downloaded on first use.
- **Plan limits on screen**: Claude and Codex usage (5h and weekly windows), Antigravity tokens per day. Agents that hit a limit wait and resume on their own.
- **Disk guard**: pauses the AIs before your disk fills up, and offers a cleanup with [Mole](https://github.com/tw93/Mole) when it gets tight.
- **8 languages**: English, Português, Español, Français, Deutsch, Italiano, 日本語 and 中文. The AIs answer in yours.
- **Everything in plain files** under `~/.tars`: one JSON per group, one JSONL per conversation.

## Install

Requires macOS, Go 1.25+ and at least one AI CLI logged in (`claude`, `codex`, `agy`, `cursor-agent`, `opencode` or `ollama`).

```sh
go install github.com/dedsfe/tars@latest
tars
```

Optional extras:

```sh
brew install ffmpeg whisper-cpp   # voice (tars offers to install it on first ctrl+r)
brew install mole                 # disk cleanup
```

## Keys

| Key | Action |
| --- | --- |
| `enter` | send (`alt+enter` for a new line) |
| `tab` | complete the `@` you're typing |
| `alt+↑` `alt+↓` | scroll the chat (or use the trackpad) |
| `ctrl+r` | talk (press again to transcribe) |
| `ctrl+v` | paste a copied screenshot |
| `ctrl+x` | stop everyone |
| `ctrl+g` | show the group |
| `ctrl+l` | clean the disk with Mole |
| `esc` | leave the chat (AIs keep working) |

Every shortcut can be changed in `~/.tars/config.toml`, created on first run with comments:

```toml
language = "auto"     # or en, pt, es, fr, de, it, ja, zh
no_mention = "lead"   # or "all": who answers when you tag nobody

[voice]
enabled = true

[disk]
warn_gb = 5
pause_gb = 2
mole = "inside"       # or "window": run Mole in place of tars, or in a new window

[keys]
voice = "ctrl+r"
```

## How it works

Each turn runs the AI's CLI headless (`claude -p`, `codex exec`, `agy -p`...) inside the group folder, with a prompt that holds the group goal, its role, the members and the last messages. The reply goes back to the chat, and any `@` in it wakes the next AI. No AI keeps hidden state: the conversation is the memory, so every member sees the same thing.

```
internal/
  engines/   detects installed CLIs and runs each one headless
  chat/      who answers, mentions, and the prompt each AI reads
  store/     groups and conversations on disk
  usage/     plan limits (Claude events, Codex session logs)
  voice/     recording (ffmpeg) and local transcription (whisper.cpp)
  attach/    images dropped or pasted into the chat
  config/    ~/.tars/config.toml
  i18n/      the 8 interface languages
  ui/        terminal screens (Bubble Tea)
```

## Safety

"Whole Mac" access runs each CLI with its own skip-permissions flag (`--dangerously-skip-permissions`, `--dangerously-bypass-approvals-and-sandbox`...). Only choose it for AIs and groups you trust. The default is the group folder only.

## License

MIT
