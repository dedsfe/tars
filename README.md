# bot

Grupos de IAs de terminal (Claude, Codex, Gemini, Antigravity, Cursor, OpenCode, Ollama) que trabalham juntas.

```
go build -o ~/.local/bin/bot . && bot
```

## Estrutura

- `main.go`: abre a tela.
- `internal/engines`: descobre quais IAs estão instaladas e os modelos de cada uma.
- `internal/store`: guarda cada grupo em `~/.bot/grupos/<id>.json` e a conversa em `<id>.chat.jsonl` (`BOT_DIR` troca a pasta).
- `internal/chat`: quem responde cada mensagem (@, @todos ou o líder) e o texto que cada IA lê.
- `internal/engines/run.go`: roda cada IA sem interface, no modo seguro dela.
- `internal/ui`: telas (lista de grupos, criação de grupo em 5 passos, detalhe do grupo).

## Criação de grupo

1. Objetivo: nome, objetivo (todas as IAs leem) e pasta de trabalho.
2. Quem entra: IAs instaladas; `+` coloca mais de uma igual.
3. Perfis: apelido (@), função, modelo, cor e acesso (só a pasta ou Mac inteiro).
4. Líder: recebe mensagens sem @ e divide o trabalho.
5. Revisão.

## Chat

- Sem @ quem responde é o líder. `@apelido` chama alguém, `@todos` chama todo mundo.
- As IAs se marcam do mesmo jeito. Quem delega é chamado de volta quando o outro termina.
- Máximo de 12 falas seguidas das IAs por mensagem sua, pra não virar loop.
- `ctrl+g` mostra o grupo, `ctrl+x` para todo mundo, `esc` sai e elas continuam trabalhando.
