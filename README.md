# bot

Grupos de IAs de terminal (Claude, Codex, Gemini, Antigravity, Cursor, OpenCode, Ollama) que trabalham juntas.

```
go build -o ~/.local/bin/bot . && bot
```

## Estrutura

- `main.go`: abre a tela.
- `internal/engines`: descobre quais IAs estão instaladas e os modelos de cada uma.
- `internal/store`: guarda cada grupo em `~/.bot/grupos/<id>.json`.
- `internal/ui`: telas (lista de grupos, criação de grupo em 5 passos, detalhe do grupo).

## Criação de grupo

1. Objetivo: nome, objetivo (todas as IAs leem) e pasta de trabalho.
2. Quem entra: IAs instaladas; `+` coloca mais de uma igual.
3. Perfis: apelido (@), função, modelo, cor e acesso (só a pasta ou Mac inteiro).
4. Líder: recebe mensagens sem @ e divide o trabalho.
5. Revisão.
