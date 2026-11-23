# AstraCalls só para ligações (Docker Compose + Caddy)

Para quem já atende as mensagens do WhatsApp em outro sistema (ex.: WAHA + Chatwoot)
e quer o AstraCalls **só para chamadas de voz**, pareado no mesmo número como mais um
aparelho. Os vendedores atendem e ligam pelo painel do AstraCalls, aberto numa aba do
navegador.

## O que muda com `WACALLS_CALLS_ONLY=true`

- Não envia presença "online": o celular e o outro aparelho continuam recebendo
  notificações normalmente.
- Não marca mensagens como lidas e não envia recibo de leitura.
- Não guarda o conteúdo das mensagens no banco (nem o histórico que chega no
  pareamento).
- Não fala com o Chatwoot, mesmo que haja configuração antiga salva.
- Não abre a porta SIP (5060).
- As rotas de envio de mensagem, disparo em massa, agendamento, status, presença,
  Chatwoot e MCP respondem `403` com `"desativado no modo só ligações"`.
- Uma chamada que toca até o fim do tempo **não é recusada**: só some do painel e o
  celular da loja continua tocando.

## Arquivos

Em [`deploy/so-ligacoes/`](../deploy/so-ligacoes/):

- `docker-compose.yml`: AstraCalls + PostgreSQL próprio (o Postgres não é exposto);
- `.env.example`: variáveis a preencher;
- `Caddyfile`: trecho para o Caddy que já existe.

## Imagem

A imagem oficial `astraonline/astracalls` **não tem** este modo. Use uma imagem gerada
a partir deste repositório (fork):

- pela CI do fork (workflow `docker-multiarch.yml`), trocando `IMAGE` para o seu
  repositório no Docker Hub e cadastrando os secrets `DOCKERHUB_USERNAME` e
  `DOCKERHUB_TOKEN`; ou
- numa máquina de build (não na VPS de produção, o build é pesado):
  `docker build -t seuusuario/astracalls:so-ligacoes .` e `docker push`.

Coloque o nome em `ASTRACALLS_IMAGE` no `.env`.

## Variáveis

Obrigatórias (no `.env`):

| Variável | Para quê |
|---|---|
| `ASTRACALLS_IMAGE` | Imagem gerada deste fork |
| `ASTRACALLS_DOMAIN` | Domínio do painel, ex.: `ligacoes.sualoja.com.br` |
| `WACALLS_API_KEY` | Chave-mestra do administrador (`openssl rand -hex 32`). Sem ela a API fica aberta e o login de atendente não protege nada |
| `POSTGRES_PASSWORD` | Senha do Postgres do AstraCalls (`openssl rand -hex 24`) |

Já definidas no compose (mude só se precisar):

| Variável | Padrão | Efeito |
|---|---|---|
| `WACALLS_CALLS_ONLY` | `true` | Liga o modo só ligações |
| `WACALLS_DEFAULT_TRANSPORT` | `websocket` | Áudio do navegador pela 443. Com `auto` o painel tenta WebRTC (UDP) e, se não conectar em 6 s, cai sozinho para WebSocket |
| `WACALLS_RING_TIMEOUT_SECONDS` | `90` neste modo | Segundos até uma chamada recebida que ninguém atendeu sair do painel. `0` desliga |
| `WACALLS_AUTO_REJECT` | `false` neste modo | `true` volta ao comportamento antigo (veja abaixo) |
| `WACALLS_DEVICE_NAME` | `AstraCalls` | Nome em "Dispositivos conectados" no celular (vale para pareamentos novos) |
| `WACALLS_MAX_CALLS` | `8` | Chamadas simultâneas por número |

### Sobre o tempo de toque (`WACALLS_RING_TIMEOUT_SECONDS`)

Fora do modo só ligações, quando uma chamada recebida passa 60 s tocando sem que o
WhatsApp avise que ela acabou, o AstraCalls **envia uma recusa (`reject`)**. Isso encerra
a ligação para quem ligou e faz o celular da loja parar de tocar, mesmo que alguém
fosse atender por lá. O mesmo acontece quando chega uma chamada com o limite de
chamadas simultâneas (`WACALLS_MAX_CALLS`) cheio.

No modo só ligações o padrão é **não recusar** (`WACALLS_AUTO_REJECT=false`) e o tempo
sobe para 90 s, acima do que o próprio WhatsApp deixa tocar: normalmente quem ligou
desiste ou alguém atende antes, e o WhatsApp avisa. Se mesmo assim o tempo passar, a
chamada só sai do painel e aparece como perdida no histórico; nada é enviado ao
WhatsApp e o celular segue tocando. Com o limite cheio, a chamada nova só não aparece
no painel.

### SIP

No modo só ligações o SIP fica desligado e a porta 5060 não é aberta. Fora desse
modo, `WACALLS_SIP_ADDR=off` também desliga.

## Portas

| Porta | Onde | Para quê |
|---|---|---|
| 80/tcp e 443/tcp | entrada, no Caddy | Painel, API, eventos (SSE) e áudio (WebSocket) |
| saída livre (TCP e **UDP**) | da VPS para a internet | Conexão com o WhatsApp e a mídia das chamadas (os relays do WhatsApp usam UDP 3478) |

Nada mais precisa ser aberto. O Postgres fica só na rede interna do compose.

Opcional: para o navegador usar WebRTC (um pouco menos de atraso que o WebSocket),
defina `WACALLS_UDP_PORT=50000`, `WACALLS_PUBLIC_IP=auto` e
`WACALLS_DEFAULT_TRANSPORT=auto`, publique `50000/udp` e `50000/tcp` no serviço
`astracalls` e libere essas portas no firewall.

## Instalação

1. Rede compartilhada com o Caddy (uma vez):
   ```bash
   docker network create caddy
   docker network connect caddy <container-do-caddy>
   ```
   Se o Caddy roda num compose, prefira declarar a rede `caddy` como `external` no
   compose dele, para sobreviver a recriações.
2. No servidor, numa pasta nova, copie `docker-compose.yml` e `.env.example`:
   ```bash
   cp .env.example .env   # preencha
   docker compose up -d
   docker compose logs -f astracalls   # deve aparecer "SIP desativado" e "HTTP server listening"
   ```
3. Acrescente o trecho do `Caddyfile` (com o seu domínio) ao Caddyfile que já atende
   o Chatwoot e recarregue o Caddy (`caddy reload` ou reinicie o container).
4. Abra `https://SEU-DOMINIO`, aba **Administrador**, e entre com a `WACALLS_API_KEY`.

## Primeiro acesso (administrador)

1. **Nova conta**, uma por número de WhatsApp. Leia o QR no celular da loja em
   *Configurações > Dispositivos conectados > Conectar dispositivo*. O WAHA continua
   conectado como outro aparelho.
2. **Atendentes** (botão no topo): crie um usuário por vendedor, com senha de pelo menos
   8 caracteres, e marque as linhas (números) que ele pode usar.
3. Passe a cada vendedor o endereço, o usuário e a senha.

Também dá para criar atendentes pela API:

```bash
curl -X POST https://SEU-DOMINIO/api/agents \
  -H "X-API-Key: $WACALLS_API_KEY" -H "Content-Type: application/json" \
  -d '{"username":"maria","name":"Maria","password":"uma-senha-boa","sessions":["ID_DA_CONTA"]}'
```

O ID da conta aparece no painel do administrador, embaixo do nome da conta.

## Dia a dia (atendente)

- Entre na aba **Atendente** com usuário e senha. Com a aba aberta o acesso se renova
  sozinho; fechada, vale por 12 horas. Se o administrador remover o atendente ou
  trocar a senha, o painel volta para a tela de entrada em até 30 segundos.
- Deixe a aba aberta. Na primeira vez, permita o **microfone** e as **notificações**
  (o navegador pergunta no primeiro clique). Com a aba em segundo plano, a chamada
  toca, o título da aba muda e aparece uma notificação.
- O painel mostra só o discador, a chamada tocando (atender/recusar), as chamadas em
  andamento (mudo, espera, transferir, desligar), as chamadas dos colegas e o
  **Histórico**, com as perdidas em vermelho e o botão **Retornar**.
- **Recusar** encerra a chamada para quem ligou e para o celular da loja. Se não quiser
  atender pelo painel, só deixe tocar.
- Cinco senhas erradas seguidas travam o usuário por 15 minutos.

## Atualização

```bash
docker compose pull && docker compose up -d
```

O histórico de chamadas e os atendentes ficam no Postgres (volume `pgdata`).

## Riscos conhecidos

- **Aparelho removido por inatividade.** Fora deste modo o AstraCalls envia presença
  "online" ao conectar e a cada 6 h, justamente para o WhatsApp não desconectar o
  aparelho por falta de uso. Aqui isso fica desligado (senão o celular deixaria de
  notificar). Se o WhatsApp desconectar o AstraCalls, o número aparece como
  "Desconectado" no painel e é preciso ler o QR de novo. Acompanhe nas primeiras semanas.
- **Contas com vários aparelhos** (issue #35 do projeto original): há relatos de
  chamadas que conectam sem áudio para quem ligou e de chamadas recebidas que não
  chegam. Teste com os números reais antes de depender disso.
