# moka-1

**moka** = **M**ixture **o**f **K**nowledge **A**gent.
An agentic RSS feed reader that starts with a single `docker compose up -d` and keeps working in the background.
Also the dogfooding project for [Plecto](https://github.com/Kaikei-e/Plecto).
Design principles live in [docs/tenets/moka-tenets.md](docs/tenets/moka-tenets.md), decisions in [docs/adr/](docs/adr/).

English · [日本語](README.ja.md)

## Getting started

```bash
git clone https://github.com/Kaikei-e/moka-1 && cd moka-1
openssl rand -base64 24 | tr -d '/+=' > secrets/postgres_password.txt  # once per clone (see secrets/README.md)
docker compose up -d --wait
```

- UI: https://localhost/ (self-signed dev certificate, so expect a browser warning)
- Edge admin: http://localhost:9099/metrics (host-local only)

The first start downloads the LLM model (~5.2 GB), so `--wait` takes a few minutes.
If the LLM is down, moka still works as a plain RSS reader (fail-soft).

## Layout (5 resident services)

| Service | Tech | Role |
|---|---|---|
| [plecto](plecto/) | Plecto (Rust + WASM filters) | Edge: TLS termination, HTTP/1·2·3, routing |
| moka-core ([core/](core/)) | Single Go binary | API + resident agent (fetch / enrich / recap) |
| moka-web ([web/](web/)) | SvelteKit SSR | The reading UI |
| db ([db/](db/)) | PostgreSQL 18 + pgvector | Articles, feeds, embeddings, job state — everything |
| llm | llama.cpp server (Vulkan) | Local inference |

Migrations are applied automatically at startup by the one-shot `migrate` job (Atlas).
