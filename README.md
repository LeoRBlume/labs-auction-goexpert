# Leilão (Auction) — Fechamento Automático

API de leilões em Go que agenda o **fechamento automático** de cada leilão. Quando um leilão é criado, uma *goroutine* em background monitora o tempo e, assim que a duração configurada expira, atualiza o status do leilão para **`Completed` (fechado)** no MongoDB — sem nenhuma intervenção manual.

A lógica principal está em [`internal/infra/database/auction/create_auction.go`](internal/infra/database/auction/create_auction.go).

## Requisitos

- [Docker](https://docs.docker.com/get-docker/) e Docker Compose
- (Opcional, apenas para rodar os testes localmente) [Go 1.20+](https://go.dev/dl/)

## Como rodar o projeto (Docker Compose)

Na raiz do projeto:

```bash
docker compose up -d --build
```

Isso sobe dois containers:

| Serviço   | Porta   | Descrição                        |
|-----------|---------|----------------------------------|
| `app`     | `8080`  | API HTTP (Gin)                   |
| `mongodb` | `27017` | Banco de dados MongoDB           |

A API fica disponível em `http://localhost:8080`.

Para acompanhar os logs da aplicação:

```bash
docker compose logs -f app
```

Para derrubar tudo (incluindo o volume do banco):

```bash
docker compose down -v
```

## Variáveis de ambiente

As variáveis são carregadas de [`cmd/auction/.env`](cmd/auction/.env) (referenciado pelo `docker-compose.yml`).

### Variáveis de tempo (foco da entrega)

| Variável                | Exemplo | Descrição                                                                                          |
|-------------------------|---------|----------------------------------------------------------------------------------------------------|
| **`AUCTION_DURATION`**  | `20s`   | **Duração do leilão.** Após esse tempo, a goroutine fecha o leilão automaticamente (status `Completed`). |
| `AUCTION_INTERVAL`      | `20s`   | Janela usada na validação de lances (bids) para rejeitar lances após o encerramento.               |
| `BATCH_INSERT_INTERVAL` | `20s`   | Intervalo do processamento em lote de lances.                                                      |
| `MAX_BATCH_SIZE`        | `4`     | Tamanho máximo do lote de lances.                                                                  |

O valor aceita qualquer formato de [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration), por exemplo: `500ms`, `20s`, `1m`, `2h`. Se `AUCTION_DURATION` estiver ausente ou inválida, o sistema usa **5 minutos** como padrão.

Para um leilão de 1 minuto, por exemplo, ajuste em `cmd/auction/.env`:

```env
AUCTION_DURATION=1m
```

e reconstrua: `docker compose up -d --build`.

### Variáveis de banco

| Variável        | Valor padrão                                                        |
|-----------------|--------------------------------------------------------------------|
| `MONGODB_URL`   | `mongodb://admin:admin@mongodb:27017/auctions?authSource=admin`     |
| `MONGODB_DB`    | `auctions`                                                          |

## Testando o fechamento automático (fim a fim)

Com a stack no ar e `AUCTION_DURATION=20s`:

**1. Criar um leilão** (retorna `201 Created`):

```bash
curl -i -X POST http://localhost:8080/auction \
  -H "Content-Type: application/json" \
  -d '{
    "product_name": "Playstation 5",
    "category": "Games",
    "description": "A brand new console in box",
    "condition": 1
  }'
```

**2. Consultar o leilão** — logo após a criação o `status` é `0` (**Active**):

```bash
curl http://localhost:8080/auction?status=0
```

**3. Aguardar o tempo de `AUCTION_DURATION`** (ex.: 20s) e consultar o leilão pelo id — o `status` passa para `1` (**Completed**), automaticamente:

```bash
curl http://localhost:8080/auction/<AUCTION_ID>
```

Valores de status: `0 = Active` (aberto) · `1 = Completed` (fechado).

`condition`: `1 = New`, `2 = Used`, `3 = Refurbished`.

## Endpoints

| Método | Rota                          | Descrição                          |
|--------|-------------------------------|------------------------------------|
| POST   | `/auction`                    | Cria um leilão                     |
| GET    | `/auction`                    | Lista leilões (filtros por query)  |
| GET    | `/auction/:auctionId`         | Busca leilão por id                |
| GET    | `/auction/winner/:auctionId`  | Busca o lance vencedor             |
| POST   | `/bid`                        | Cria um lance                      |
| GET    | `/bid/:auctionId`             | Lista lances de um leilão          |
| GET    | `/user/:userId`               | Busca usuário por id               |

## Teste automatizado

Há um teste que comprova o fechamento automático em
[`internal/infra/database/auction/create_auction_test.go`](internal/infra/database/auction/create_auction_test.go).
Ele usa um deployment mockado do MongoDB (`mtest`), então **não precisa de banco no ar**: cria um leilão, aguarda a `AUCTION_DURATION`, e verifica que a goroutine emitiu o `update` fechando o leilão (`status = Completed`).

Rodar os testes:

```bash
go test ./... -v
```

Rodar apenas o teste de fechamento:

```bash
go test ./internal/infra/database/auction/ -run TestCreateAuction_ClosesAfterDuration -v
```

## Como funciona

Ao criar um leilão, `CreateAuction` dispara uma goroutine (`go ar.scheduleAuctionClose(...)`) — assim a thread principal / request HTTP **nunca fica bloqueada**. A goroutine:

1. Calcula o horário de término (`timestamp + AUCTION_DURATION`);
2. Espera com um `time.Timer` até esse horário;
3. Executa um `UpdateOne` alterando o `status` do leilão para `Completed`, usando um `context.Background()` próprio (a goroutine sobrevive ao contexto da requisição).
