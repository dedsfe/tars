# tars

**Suas IAs de terminal trabalhando juntas num grupo de chat.**
Claude, Codex, Antigravity, Cursor, OpenCode e Ollama na mesma sala: todos leem a mesma conversa, se marcam com `@`, passam trabalho uns pros outros e marcam **você** quando precisam de uma decisão.

Sem Electron, sem servidor, sem conta. Um binário em Go que comanda as IAs de terminal que você já tem instaladas.

[English](README.md)

## Por quê

Cada IA é boa numa coisa. Em vez de copiar e colar entre cinco abas, você cria o grupo uma vez, dá uma função pra cada IA e conversa com todas num lugar só.

## O que ele faz

- **Criar grupo em 5 passos**: objetivo (toda IA lê), quem entra, perfil de cada IA (`@apelido`, função, modelo, cor, acesso), líder e revisão.
- **@ em tudo**: `@dev faz X` acorda só o `@dev`; `@todos` acorda todo mundo; sem @ vai pro líder (ou pra todos, se preferir).
- **As IAs se marcam**: quem delega é chamado de volta com o resultado. Depois de 12 falas seguidas das IAs, o tars para pra não virar loop.
- **Elas te marcam**: chega um aviso do macOS e o grupo ganha um selo na lista.
- **Dois níveis de acesso por IA**: só a pasta do grupo (modo seguro de cada CLI) ou o Mac inteiro, que pula todas as permissões *porque você escolheu*. Com o [cua-driver](https://github.com/trycua/cua) elas também usam o navegador e os apps em segundo plano.
- **Imagens**: arraste o arquivo pro terminal ou aperte `ctrl+v` com um print copiado.
- **Voz**: `ctrl+r` grava e transcreve no próprio Mac com o Whisper. Nada sai do seu computador. Opcional, baixado no primeiro uso.
- **Limite do plano na tela**: uso do Claude e do Codex (janelas de 5h e da semana) e tokens do Antigravity no dia. Quem estoura o limite espera e volta sozinho.
- **Proteção de disco**: pausa as IAs antes do disco lotar e oferece limpeza com o [Mole](https://github.com/tw93/Mole).
- **8 idiomas**: English, Português, Español, Français, Deutsch, Italiano, 日本語 e 中文. A interface segue o idioma do seu Mac (ou `language = "pt"` no config) e as IAs respondem no mesmo idioma.
- **Tudo em arquivos simples** em `~/.tars`: um JSON por grupo, um JSONL por conversa.

## Instalar

Precisa de macOS, Go 1.25+ e pelo menos uma IA de terminal logada (`claude`, `codex`, `agy`, `cursor-agent`, `opencode` ou `ollama`).

```sh
go install github.com/dedsfe/tars@latest
tars
```

Extras opcionais:

```sh
brew install ffmpeg whisper-cpp   # voz (o tars oferece instalar no primeiro ctrl+r)
brew install mole                 # limpeza de disco
```

## Teclas

| Tecla | O que faz |
| --- | --- |
| `enter` | envia (`alt+enter` pula linha) |
| `tab` | completa o `@` que você está digitando |
| `alt+↑` `alt+↓` | rola a conversa (ou use o trackpad) |
| `ctrl+r` | fala (aperte de novo pra transcrever) |
| `ctrl+v` | cola um print copiado |
| `ctrl+x` | para todo mundo |
| `ctrl+g` | mostra o grupo |
| `ctrl+l` | limpa o disco com o Mole |
| `esc` | sai do chat (as IAs continuam trabalhando) |

Todas as teclas mudam em `~/.tars/config.toml`, criado comentado na primeira vez que o tars abre.

## Como funciona

Cada vez que uma IA fala, o tars roda a CLI dela sem interface (`claude -p`, `codex exec`, `agy -p`...) dentro da pasta do grupo, com um texto que traz o objetivo, a função dela, os membros e as últimas mensagens. A resposta volta pro chat, e cada `@` nela acorda a próxima IA. Nenhuma IA guarda estado escondido: a conversa é a memória, então todo mundo vê a mesma coisa.

## Segurança

O acesso "Mac inteiro" roda cada CLI com a opção de pular permissões dela (`--dangerously-skip-permissions`, `--dangerously-bypass-approvals-and-sandbox`...). Use só com IAs e grupos em que você confia. O padrão é só a pasta do grupo.

## Licença

MIT
