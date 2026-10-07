# Refinement como rules do crew

Rascunho para evoluir. Aplica o modelo de [`ce-brainstorm-crew-rules.md`](ce-brainstorm-crew-rules.md) a um fluxo mais curto: a refinement que o #281 transforma em `/cw-refine`.
Só nomeia as ações; nenhuma está escrita.

A refinement é quase linear e só precisa de uma pessoa nos casos de dúvida.
Os atores são os mesmos do rascunho do brainstorm (boss, crew, script, oráculo, facilitador, pesquisador); aqui o boss quase não aparece.

## Orientada a eventos, sem esperar

Esperar só faz sentido para uma LLM: uma sessão parada guarda o contexto e quase não gasta token.
Um script não tem contexto a guardar.
Ele pode terminar e deixar que a mudança de estado acorde a próxima rule, que é como o crew funciona hoje: várias rules reagindo a labels.

- **Não há `await`.** Quando um script precisa de uma resposta lenta, ele faz o pedido e termina com o veredito `asked_<papel>`, que leva o issue ao estado "perguntado a esse papel": um label, com o crew de hoje, ou um estado interno, na versão com marcos (abaixo).
- **Quem responde também reage a um estado.** Cada papel lento pega esse estado, responde os pedidos abertos e devolve o issue ao estado de quem perguntou. A volta é fixa: não precisa de rule de despacho.
- **O script de cada fase é idempotente.** Ao voltar, ele roda de novo, lê no thread o que já foi respondido e segue de onde parou.
- **O oráculo responde dentro da run.** O judge responde em segundos, então chamá-lo é como chamar uma função: o script pergunta e recebe a resposta. As duas ficam registradas no thread, e o script só conhece o papel, não o Jev. Um salto de estado só vale a pena para um papel lento.
- **O boss não tem rule.** O issue fica no estado "perguntado ao boss" até a resposta dele chegar (ver [Como o crew percebe o que vem de fora](#como-o-crew-percebe-o-que-vem-de-fora)).
- **A única espera que sobra é a da LLM:** o `wait:` do #255, para uma sessão que trava no meio do próprio pedido e pergunta.

O padrão de cada fase:

```text
crew:<fase>:ready ──► rule <fase> [script] ──asked_researcher──► crew:<fase>:asked researcher
       ▲                                                                   │
       └──────────── passed ◄── rule <fase> researcher [sessão] ◄──────────┘
```

## Marcos no tracker, chat no crew

Uma equipe remota tem um chat além do tracker, e nem todo estado precisa ir para o tracker.
O estado fica em duas camadas:

| Camada | O que guarda | Quem vê | Onde fica |
|---|---|---|---|
| **Marco** (tracker) | estados estáveis que uma pessoa precisa ver ou agir: `ready`, `running`, `waiting answer`, `waiting review`, `done`, `failed` | a equipe toda, o board | label do issue |
| **Chat** (interno) | os passos dentro de um marco: "perguntei ao pesquisador", "a proposta de split chegou", "faltam as dependências" | o crew, o live view, os logs | eventos locais do crew |

Na refinement, os estados `asked …` da versão com um label por estado (abaixo) viram chat.
O issue fica em `crew:refinement:running` do começo ao fim e só sai daí para um marco: `done`, `waiting review`, `failed`, ou `waiting answer` quando precisa do boss.

### Onde mora o chat

- **Memória:** rápida, mas some se o crew reiniciar. Sozinha não serve.
- **O run journal do crew:** o candidato natural. O crew já tem um log local de eventos (`.crew/`, jsonl, versão 3); o core já é um redutor de (modelo, entrada) para (comandos, eventos); o `History` já reconstrói o estado de cada issue a partir desses eventos. Pedidos e respostas internos seriam mais dois tipos de evento de run, como `Asked{to, name, about}` e `Answered{q, value}`, e o R22 do #254 já retoma a partir deles.
- **Commits numa branch intermediária:** para os artefatos (a proposta de split, o `grounding.md`, o contrato do plano). A worktree e a branch de cada rule run já existem (R24 do #254). Um commit por transição deixa um histórico que dá para inspecionar e sobrevive à troca de máquina.

Os eventos ficam no journal e os artefatos na branch.

O chat continua visível sem ir para o tracker: o live view do crew pode ser a janela dessa conversa.
Os pedidos e respostas do oráculo não precisam virar comentário no GitHub, porque o ledger do #203 já os guarda.
O tracker recebe o que interessa à equipe: a pergunta ao boss e o comentário de fechamento.

### Como o crew percebe o que vem de fora

- **Por polling, como hoje.** A cada tick, o crew já lista os issues dos labels `ready` e `running`. Basta mais uma chamada por repositório, `GET /repos/{owner}/{repo}/issues/comments?since=<último tick>`, que traz todos os comentários novos de uma vez. O crew distribui cada um ao issue em espera: uma resposta que conta (R37 a R43 do #255) a um pedido aberto vira o evento `Answered`. Com isso, o boss não move mais label: o crew tira o issue de `waiting answer` e o devolve a `running` sozinho, em até um ciclo. A revisão de um PR entra pelo mesmo caminho.
- **Por webhook, depois.** Rodando local, não há endpoint público. Existe uma extensão do `gh` que encaminha webhooks do repositório para localhost (`cli/gh-webhook`, em beta; conferir se ainda é mantida). Não precisa começar por aí.

Os passos internos não esperam o poll: são eventos no próprio processo e podem rodar no mesmo tick.
Só o que vem de fora (o boss, o PR) leva um ciclo, o que elimina o custo dos saltos da versão com um label por estado.

### A refinement com marcos

```text
label (marco)                  chat (interno, no journal)         quem reage
──────────────────────────────────────────────────────────────────────────────────
crew:refinement:ready          —                                  crew: take
crew:refinement:running        dups                               script refinement
                               asked researcher · dups            sessão researcher
                               split                              script split
                               asked facilitator · split          sessão facilitator
                               deps                               script deps
                               asked researcher · deps            sessão researcher
crew:refinement:waiting answer asked boss                         ninguém: o crew vê a
                                                                  resposta no feed e volta
                                                                  para running
crew:refinement:done | waiting review | failed
```

Os labels voltam a ser os quatro ou cinco de qualquer rule.
`split` e `deps` deixam de ser rules com labels próprios e viram estados internos da rule `refinement`; as reações a cada estado são as mesmas ações da versão abaixo.

### O que falta no crew

1. **Estados internos que disparam reações.** Hoje uma rule só é disparada por label, e o #254 deixou de fora disparar por outra coisa, e também saltos e laços dentro da rule. Estados internos são isso, sem label. É a mudança maior, e reabre uma decisão do #220.
2. **Pedido e resposta como eventos de run,** estendendo o journal e o agregado de rule run do #237.
3. **O feed de comentários do repositório** no tick, com o filtro de quem pode responder do #255, e o crew movendo `waiting answer` ↔ `running` sozinho.

Até o item 1 existir, um meio-termo é um script orquestrador dentro da run, que chama o oráculo e roda `claude -p` para o pesquisador.
Funciona com o crew de hoje, mas as sessões ficam fora do crew (bot, log, slot) e o script vira um segundo motor: serve só para provar a ideia.

As seções seguintes trocam o item 1 por algo menor: comandos em comentários, o caminho escolhido, ou um segundo tracker, deixado de lado.

## Comandos em comentários

Numa equipe remota e assíncrona, o GitHub basta: não precisa de chat.
O que falta é outra forma de reagir além de labels: um comando num comentário, como o `/trunk merge` que este repositório já usa com o Trunk.
Os estados intermediários viram comandos; os labels ficam só com os marcos.
O crew ganha um watcher de comentários e distribui o trabalho do mesmo jeito que distribui olhando os labels.

### O gatilho

Uma rule pode ser disparada por um label, como hoje, ou por um comando num comentário:

```yaml
rules:
  split facilitator:
    trigger: { command: "/crew propose-split" }
    actions:
      - agent: product-manager
    routes:
      passed: [comment: "/crew check-split"]
```

- **Uma rule de comando não tem labels.** Não move label quando pega o issue, e suas rotas terminam postando o próximo comando, com o `comment` que o #254 já tem e um texto do config. O R15 do #254 ("toda rota termina com `move` ou `close`") passa a valer só para as rules de label.
- **O issue fica no `running` em que a rule de entrada o deixou** até a última rule mover para um marco. Não precisa de um label `working`: o crew só pega issues de labels `ready` ("Only the ready labels are taken from; the others fill the default board", em `listIssues`, `internal/core/scheduler.go`) e nunca retoma sozinho (CONCEPTS.md). Um issue em `running` sem run é só um card parado no board, e diz a verdade: a refinement está em andamento.

### O watcher

A cada tick, uma chamada por repositório traz os comentários novos: `GET /repos/{owner}/{repo}/issues/comments?since=<último tick>` para issues e PRs, e `pulls/comments?since=…` para os de revisão.
Para cada comentário que começa com `/crew`:

1. **Quem pode comandar:** o filtro do #255 (code owners e os bots da lista). Um `/crew …` de um estranho num repositório público é ignorado sem resposta.
2. **Qual rule:** a que tem o `trigger.command` que bate.
3. **Se dá para pegar agora:** como hoje, uma run por issue e vaga na fila da rule. Senão, o comando espera o próximo tick.
4. **Uma vez só:** o crew grava no journal o id do comentário tratado.

O crew reage no comentário do comando, como o Trunk: 👀 quando pega, 🚀 quando termina, 😕 quando falha.
Dá para ver o estado sem label.

Os argumentos de `/crew research dep #277` chegam à rule pelo template (`{{.Command.Args}}`) e pelo ambiente (`CREW_COMMAND_ARGS`), nunca interpolados num script: são texto de fora, mesmo de alguém de confiança.

### O #281 com comandos

```text
#281  Turn the pm-triage prompt into a skill                  crew:refinement:ready

crew-clerk           label → crew:refinement:running
                     (rule refinement) sem duplicata; plano acima do limite
crew-clerk           /crew propose-split                           👀 🚀
crew-product-manager proposta: parte 1 (R1–R16), parte 2 (R17–R21), blocked_by parte 1
                     /crew check-split                             👀 🚀
crew-clerk           (rule split) kept whole: a parte 1 não mergeia sozinha
                     /crew deps                                    👀 🚀
crew-clerk           (rule deps) #254 blocks, #285–#287 blocked_by; #277 unsure
                     /crew research dep #277                       👀 🚀
crew-researcher      blocks: o #284 tira de internal/config os testes que o R20 reescreve
                     /crew deps                                    👀 🚀
crew-clerk           (rule deps) links gravados · comentário de fechamento
                     label → crew:refinement:done
```

No GitHub, o #281 passa só por `ready`, `running` e `done`; os passos do meio são comentários, e a equipe vê tudo no thread.
Cada comando custa até um poll: os 5 saltos somam até uns 25 minutos.

O boss entra do mesmo jeito: responde com `/crew answer 2`, manda seguir com `/crew deps` ou dispara uma rule à mão com `/crew refine`.
Uma resposta em texto livre ainda pode contar (pelas regras do #255), mas o comando é determinístico e ninguém precisa interpretá-lo.

O mesmo vale para PRs: o crew já trata PRs como itens, então um `/crew address-review` num PR pode disparar a rule que responde a revisão.

### Comparado com o chat

| | Chat (segundo tracker) | Comandos em comentário |
|---|---|---|
| Adapter novo | o `local` | nenhum |
| Config | `trackers:` e `move` entre trackers | `trigger: { command: … }` |
| Onde ficam os passos do meio | fora do GitHub | no thread, visíveis para a equipe |
| Custo de um salto | nenhum (local) | até um poll |
| Resposta do boss | ainda precisa do feed de comentários | o mesmo watcher já cobre |

O watcher que os comandos pedem é o mesmo feed de comentários que faltava para perceber a resposta do boss: uma peça resolve as duas coisas.

### O que o crew precisa

1. **O watcher de comentários no tick**, para issues e PRs.
2. **O gatilho por comando nas rules**, com argumentos, e rules sem labels. Reabre uma decisão do #254, que deixou de fora "rules triggered by anything other than a label".
3. **O filtro de quem pode comandar**, reaproveitando o R37 a R43 do #255.
4. **Comandos tratados uma vez só:** o id do comentário no journal, e as reações como sinal.

## Alternativa deixada de lado: o chat como um segundo tracker

Fica registrada como alternativa: os comandos em comentários resolvem o mesmo com menos peças e sem tirar nada do GitHub.

As rules podem continuar funcionando como hoje: o chat vira só mais um tracker.
Os estados intermediários são labels num tracker local, e os pedidos e respostas são comentários nele.
Toda rule continua disparada por um label, e cada uma diz de qual tracker lê.
Não reabre a decisão do #254 ("rules triggered by anything other than a label" segue fora do escopo): o gatilho continua sendo um label.

O código já aponta nessa direção:

- o `Tracker` é um port;
- o `internal/fake` já tem um tracker em memória (`NewTracker`) e um que comenta, fecha e lista comentários (`NewRoutingTracker`);
- o `IssueID` já carrega o repositório além da chave, então itens de dois trackers não se confundem.

### O config

```yaml
trackers:
  github: { name: github, bot: clerk }
  chat:   { name: local, store: .crew/chat }      # ou uma branch git, ver abaixo

rules:
  refinement:
    tracker: github
    labels: { ready: "crew:refinement:ready", running: "crew:refinement:running" }
    actions: [check-unfinished-split, dup-candidates, ask-oracle-dups, pick-dup, measure-plan]
    routes:
      duplicate: [comment, move: "crew:refinement:waiting review"]
      large:
        - move: { tracker: chat, label: "split:ready" }     # cria o item local, ligado ao issue
      passed:
        - move: { tracker: chat, label: "deps:ready" }

  split:
    tracker: chat
    labels: { ready: "split:ready", running: "split:running" }
    actions: [need-proposal, validate-split, ask-oracle-parts, pick-split, ask-oracle-moves, create-parts]
    routes:
      asked_facilitator: "split:asked facilitator"
      kept_whole:        "deps:ready"
      split:
        - move: { tracker: github, label: "crew:split:done" }
        - close                                             # fecha o item local

  split facilitator:
    tracker: chat
    labels: { ready: "split:asked facilitator", running: "split:facilitator running" }
    actions: [{ agent: product-manager }]
    routes: { passed: "split:ready" }

  deps:
    tracker: chat
    # ... como na versão com um label por estado, e no fim:
    routes:
      passed:
        - move: { tracker: github, label: "crew:refinement:done" }
        - close
```

No GitHub, o #281 passaria só por `ready`, `running` e `done`.
Todo o resto acontece no `chat`.

### O que o crew precisa ganhar

1. **Vários trackers no config.** Hoje há um `tracker:` só. Passaria a ser um mapa `trackers:`, com `tracker:` em cada rule.
2. **Um `move` que atravessa trackers.** No primeiro `move` para o `chat`, o crew cria o item local ligado à origem (o `IssueID` do issue no GitHub). O `close` encerra o item local no fim.
3. **Os templates apontam para a origem.** Numa rule do `chat`, `{{.Issue.Ref}}` continua dizendo `#281`, para prompts e comentários falarem do issue de verdade.
4. **O issue fica em `running` no GitHub** enquanto a cadeia está no `chat`, como na versão com comandos: o crew só pega issues de labels `ready` e nunca retoma sozinho.
5. **Um jeito de as sessões falarem com o `chat`.** Hoje elas usam o `gh`. Para o tracker local precisariam de um comando, como `crew tracker chat issue 281 comment`, ou de arquivos no ambiente da sessão.
6. **O adapter `local`**, com três opções de onde guardar:
   - **memória:** é o fake. Serve para testes, mas some num restart;
   - **arquivo em `.crew/`:** jsonl, como o journal. Durável e local;
   - **commits numa branch:** dá para inspecionar, sobrevive à troca de máquina e é o lugar natural dos artefatos.

Uma opção sem adapter novo: o `chat` é o mesmo adapter `github`, apontado para outro repositório, privado, só para o chat.
O custo é que cada salto volta a gastar API e a esperar o poll.

O poll no tracker local não chama API nenhuma: pode rodar a cada tick sem custo, ou o crew pode pegar o item na hora em que uma rota o move.

### O que continua igual

Uma pergunta ao boss continua no GitHub: uma rota posta a pergunta e move o issue para `crew:refinement:waiting answer`.
Perceber a resposta continua sendo o feed de comentários do GitHub, ou o boss movendo o label de volta.

## Funções do tracker

Quase tudo o que os scripts fazem com `gh` é trabalho do tracker: listar issues, ler, comentar, mover label, ligar issues.
Um script que chama `gh` direto só funciona no GitHub; uma função do tracker passa pelo port e funciona em qualquer tracker.
O #254 já tem quatro funções assim, como efeitos de rota: `move`, `comment`, `report` e `close`.
A ideia é generalizar: funções do tracker que também podem ser ações, inclusive as de leitura, que devolvem dados.

### As ações da refinement, classificadas

| Ação de hoje | Vira | Quem faz |
|---|---|---|
| listar issues abertos e fechados, por autor e motivo | `tracker.issues` | função do tracker |
| listar PRs mergeados com os arquivos | `tracker.pulls` | função do tracker |
| ler issue, corpo e comentários | `tracker.issue` | função do tracker |
| sub-issues de um issue | `tracker.sub_issues` | função do tracker |
| ler, gravar e remover `blocked_by` | `tracker.dependencies` / `link` / `unlink` | função do tracker |
| criar as partes, com label e pai | `tracker.create` | função do tracker |
| comentar, mover label, fechar | `comment`, `move`, `close` | já são efeitos do #254 |
| pedir e ler as respostas que contam | `tracker.ask` / `tracker.answers` | função do tracker, com o filtro do #255 |
| perguntar ao oráculo | `judge.ask` | função do judge |
| ranquear duplicatas, sobreposição de arquivos | `rank-dups` | script puro |
| medir o plano | `measure-plan` | script puro (o `measure.sh`) |
| validar o split, checar ciclos | `validate-split`, `plan-links` | script puro |
| escolher o veredito | `pick-*` | script puro |
| montar o comentário final | `render-closing` | script puro, ou o template do próprio `comment` |
| propor partes, evidência | — | sessão |

Um script puro recebe JSON e devolve JSON e um veredito: não chama `gh` nem a rede.

### A refinement assim

```yaml
rules:
  refinement:
    tracker: github
    actions:
      - tracker.sub_issues: { issue: "{{.Issue.Key}}", marker: "cw-split-plan: part of" }
      - check-unfinished-split           # script: lê as partes e o label → failed
      - tracker.issues:  { state: open, authors: owners_and_bots }
      - tracker.issues:  { state: closed, reason: completed }
      - tracker.pulls:   { state: merged, files: true }
      - rank-dups                        # script: BM25, tokens raros, títulos, arquivos → top 10
      - judge.ask:       { question: dup.kind, about: rank-dups, top: 10 }
      - pick-dup                         # script → duplicate · asked_researcher
      - measure-plan                     # script → large
    routes: ...

  deps:
    trigger: { command: "/crew deps" }
    actions:
      - tracker.issues:       { state: open, authors: owners_and_bots }
      - tracker.dependencies: { issue: "{{.Issue.Key}}" }
      - judge.ask:            { question: [dep.blocks, dep.blocked_by], about: tracker.issues }
      - judge.ask:            { question: dep.relation, top: 5 }
      - need-research         # script → asked_researcher
      - plan-links            # script: o que gravar e remover, sem ciclo
      - tracker.link:         { from: plan-links }
      - tracker.unlink:       { from: plan-links }
      - comment:              { template: closing }
```

Uma função recebe os parâmetros e grava a saída num diretório de dados da run, com o nome da ação; os scripts leem dali.
É o mesmo padrão do `CREW_PROMPT_FILE` e do `CREW_LAST_MESSAGE_FILE` que as ações shell já recebem.

### O que se ganha

- **Scripts pequenos, testáveis sem stub de `gh`:** basta um JSON de fixture. O R6 do #281 pede testes com stub de `gh` em CI; com isso, o stub só é necessário nas funções do crew, que já são testadas com o `internal/fake`.
- **O crew já cuida do que importa numa escrita no tracker:** retry e "owed" pelo outbox, o bot que escreve, o marcador do crew em cada comentário (R46 do #255) e o texto limpo onde entra no crew (o NUL que congelava o status, em `docs/solutions/`). Com `gh` solto num script, cada script precisa lembrar disso sozinho.
- **Menos chamadas de API:** o crew já lista issues a cada tick, e o `tracker.issues` pode reaproveitar essa listagem.
- **Checagem quando o config carrega:** as capacidades opcionais do tracker já são interfaces separadas, achadas por type assertion (`PullRequestFinder`, `CodeOwnerFinder`…). Uma função declara de qual capacidade precisa, e o crew recusa uma rule cujo tracker não a tem, como `tracker.pulls` numa rule do `chat`.

### O que falta

- **O mecanismo do #256 com funções de verdade.** O #256 entrega o mecanismo sem nenhuma função e deixa fora do escopo "Calls to external services as effects or functions". As funções do tracker passam pelo port, como os quatro efeitos que o #254 já tem; o `judge.ask` passa pelo port do judge, que é opt-in. As duas precisariam de uma decisão explícita de que não são "serviço externo" nesse sentido.
- **Saída de dados de uma ação.** Hoje uma ação devolve só um veredito, mais o arquivo da última mensagem de uma sessão. Uma função de leitura precisa de um lugar para gravar o que leu, e as ações seguintes precisam saber onde ler.
- **Uma lista de funções que sirva a qualquer tracker.** Pelo design agnóstico do crew, ela não pode copiar a API do GitHub. Precisa dos conceitos que qualquer tracker tem: itens, labels, comentários, ligações entre itens, filhos. PR e arquivos ficam como capacidade opcional.

## Com o crew de hoje: um label por estado

Sem estados internos, cada estado vira um label e cada fase uma rule.
Funciona com o #254 como está, ao custo de um poll por salto e de muitos labels.

```yaml
refinement:                          # ready: crew:refinement:ready             [script]
  - check_unfinished_split           # sub-issues com o marcador de parte e o pai ainda no crew → failed
  - dup_candidates                   # listar, ranquear, sobreposição de arquivos (ver abaixo)
  - ask_oracle_dups                  # dup.kind no top 10, dentro da run
  - pick_dup                         # full → duplicate · unsure sem resposta → asked_researcher
                                     # partial → anota a sobreposição
  - measure_plan                     # o measure.sh que já existe → large · passed
  routes:
    duplicate:        [comment, move: "crew:refinement:waiting review"]
    asked_researcher: "crew:refinement:asked researcher"
    large:            "crew:split:ready"
    passed:           "crew:deps:ready"
    failed:           [report, move: "crew:refinement:failed"]

refinement researcher:               # ready: crew:refinement:asked researcher   [sessão]
  - agent: researcher                # "responda os pedidos abertos endereçados a você no #N"
  routes: { passed: "crew:refinement:ready" }

split:                               # ready: crew:split:ready                   [script]
  - need_proposal                    # sem proposta no thread → pede ao facilitador → asked_facilitator
  - validate_split                   # cada R numa parte só, seções compartilhadas copiadas, links sem ciclo
                                     # inválida → pede revisão (máx. 2) → asked_facilitator · failed
  - ask_oracle_parts                 # part.merges_alone, part.one_unit, dentro da run
  - pick_split                       # → kept_whole (grava o porquê)
  - ask_oracle_moves                 # dep.part: qual parte cada dependência do pai toca, dentro da run
  - create_parts                     # sub-issues, o label das partes vindo da entrada, blocked_by entre
                                     # partes, registro no pai, dependências do pai movidas → split
  routes:
    asked_facilitator: "crew:split:asked facilitator"
    kept_whole:        "crew:deps:ready"
    split:             "crew:split:done"          # nenhuma rule pega: o pai sai do crew
    failed:            [report, move: "crew:split:failed"]

split facilitator:                   # ready: crew:split:asked facilitator       [sessão]
  - agent: product-manager           # escreve a proposta de partes em JSON, ou a revisão
  routes: { passed: "crew:split:ready" }

deps:                                # ready: crew:deps:ready                    [script]
  - dep_candidates                   # listar; dep.blocks e dep.blocked_by pelo oráculo, dentro da run
  - ask_oracle_relations             # dep.relation no top 5 de cada lado, dep.still_holds nos links que existem
  - need_research                    # unsure sem resposta do pesquisador → asked_researcher
  - record_deps                      # POST/DELETE, pula o que existe, recusa ciclo;
                                     # null do pesquisador → needs-human: pergunta ao boss e não espera
  - closing_comment                  # o comentário do R15, montado do thread por template
  routes:
    asked_researcher: "crew:deps:asked researcher"
    passed:           "crew:refinement:done"
    failed:           [report, move: "crew:deps:failed"]

deps researcher:                     # ready: crew:deps:asked researcher         [sessão]
  - agent: researcher
  routes: { passed: "crew:deps:ready" }
```

São seis rules: três de script e três que são, cada uma, uma sessão curta que só responde.
Nenhuma espera.

Hoje, uma sessão longa do Opus faz os 10 passos do prompt da refinement.
Aqui:

- **O facilitador** entra só quando o plano passa do limite, uma vez por proposta.
- **O pesquisador** entra só nos pares que o oráculo deixou em dúvida e nos PRs mergeados candidatos a duplicata.
- **O comentário final** sai de um template. Tudo o que o R15 do #281 pede já é dado no thread: o veredito, as duplicatas, as partes, os links e os comandos que falharam.

### O custo

- **Cada salto custa até um poll.** O crew só lista issues no tick, hoje a cada 300 s. Os 6 saltos do #281 (ver abaixo) somam até uns 30 minutos; a refinement real do #281 levou 5 (17:06 a 17:11). Numa equipe assíncrona isso pode ser aceitável. Se não for, cabe uma melhoria pequena no crew: quando uma rota move o issue para o `ready` de outra rule, o crew o pega na hora, sem esperar o próximo poll.
- **Muitos labels.** Cada fase tem `ready`, `running`, um `asked <papel>` por papel lento e os labels de fim. O board mostraria todos, a não ser que tenha colunas próprias.
- **Nomes.** `split` e `deps` são nomes de esboço; a convenção `crew:<rule>:<estado>` pede escolher os de verdade.

## Candidatos a duplicata só com script

### Listar

```text
dup_candidates, a listagem
  gh issue list --state open    → 62 issues abertos (filtra por $CREW_CODE_OWNERS e $CREW_BOTS)
  gh issue list --state closed  → 72 fechados: 63 COMPLETED, 9 NOT_PLANNED
  gh pr list --state merged     → 104 PRs, com --json files
```

Os números são deste repositório em 2026-10-07: uns 237 candidatos para cada issue refinado.
Mandar todos ao oráculo daria 237 perguntas por run; o `rank.sh` de hoje faz 46 e já é caro.
Por isso o script precisa afunilar.

Pelo R10 do #281, entram só os abertos, os fechados `COMPLETED` (trabalho entregue) e os PRs mergeados.
Um fechado `NOT_PLANNED` não entregou nada.

### Afunilar

Um protótipo, que ficou fora do repositório, dá a cada candidato três notas e soma as posições de cada um nos três rankings:

1. **Palavras (BM25)** do título com o texto próprio do issue.
2. **Tokens de código em comum**: trechos em crase, caminhos de arquivo e, nos PRs, os arquivos que eles tocam. Um token raro no repositório pesa mais.
3. **Palavras do título em comum.**

O que mais fez diferença foi comparar só as seções próprias de cada issue: título, Summary e Requirements, ou o corpo inteiro nos templates curtos.
As partes de um split copiam o Goal Capsule, o Problem Frame e as Sources inteiros.
Comparando o corpo todo, as irmãs de um split parecem iguais, e o #224 caía para o 5º lugar, atrás delas.

### O teste

Os #224, #225 e #226 foram fechados quando o #220 foi dividido de novo como #254, #255 e #256, e o #227 foi incorporado ao #255.
Como são `NOT_PLANNED`, o filtro do R10 os deixaria de fora da lista real; aqui eles servem só de gabarito de que o ranking acha o mesmo trabalho.

| Issue | Duplicata conhecida | Posição entre 237 |
|---|---|---|
| #254 | #224 (mesmo título, split anterior) | 1º |
| #255 | #225 | 1º |
| #255 | #227 (incorporado) | 2º |
| #256 | #226 | 3º |
| #281 | #277 (sobreposição parcial) | 48º |
| #281 | PR #284 (o PR do #277) | 44º |

### O que os números dizem

- **Para o mesmo trabalho, o script basta para fazer a shortlist.** As quatro duplicatas de verdade ficaram entre os 3 primeiros. Um top 10 cobre esses casos, e o oráculo julga 10 pares em vez de 237.
- **Ele não acha uma sobreposição estreita.** O #277 só compartilha com o #281 um arquivo de teste. Para isso serve a sobreposição de arquivos do `dup_candidates`, também só código: os arquivos que o plano cita contra os arquivos que os PRs tocam. O `internal/config/config_own_test.go`, citado no #281, só aparece em 5 PRs, e um deles é o #284. Com peso pela raridade do arquivo, a lista acha o #284 direto. Esse tipo de sobreposição é mais conflito ou dependência do que duplicata: foi como bloqueador que a refinement real registrou o #277.
- **Excluir a família do issue não funciona.** O #224 também é parte do #220, de um split anterior, e é a duplicata do #254. O script só exclui o próprio issue e as partes ou o pai dele, nunca os irmãos.

### Cuidados

- **Amostra pequena.** Foram 6 pares num repositório só. O tamanho do top e o peso de cada sinal precisam ser escolhidos em dados separados para teste.
- **Depende dos cabeçalhos dos templates.** O script lê seções que este repositório escreve. Em outro repositório, quais são as seções próprias seria configuração.

## O #281 refinado nesse modelo

O exemplo usa os números reais da refinement do #281. As probabilidades são inventadas.

```text
#281  Turn the pm-triage prompt into a skill                       crew:refinement:ready

── refinement ──────────────────────────────────────────────────────────────────────────
crew-clerk           label → crew:refinement:running
                     check_unfinished_split: nenhuma parte encontrada
                     dup_candidates: top 10 de ~237, a começar por #165, #285, #286;
                     sobreposição de arquivos com o PR #284 (internal/config/config_own_test.go)
crew-clerk           ‹ask r-1..10 → analyst · dup.kind›
crew-analyst         ‹answer r-1..10› todos none                          ← dentro da run
                     pick_dup: nenhuma duplicata; a sobreposição com o #284 vai para o comentário
                     measure_plan: 15.046 caracteres, 21 requisitos → large
crew-clerk           label → crew:split:ready

── split ───────────────────────────────────────────────────────────────────────────────
crew-clerk           label → crew:split:running
crew-clerk           ‹ask r-11 → facilitator · split›
crew-clerk           label → crew:split:asked facilitator                 ← a run termina

── split facilitator ───────────────────────────────────────────────────────────────────
crew-product-manager ‹answer r-11› parte 1: o padrão e /cw-refine (R1–R16)
                                   parte 2: a ligação na rule (R17–R21), blocked_by parte 1
crew-clerk           label → crew:split:ready

── split, de novo ──────────────────────────────────────────────────────────────────────
                     need_proposal: a proposta está no thread · validate_split: ok
crew-clerk           ‹ask r-12..15 → analyst · part.merges_alone, part.one_unit›
crew-analyst         ‹answer r-12› parte 1 merges_alone: não (0,88)       ← dentro da run
                     pick_split: kept_whole, "a parte 1 não mergeia sozinha"
crew-clerk           label → crew:deps:ready

── deps ────────────────────────────────────────────────────────────────────────────────
crew-clerk           ‹ask r-16..105 → analyst · dep.blocks, dep.blocked_by› 45 pares
crew-analyst         ‹answer› bloqueia o #281: #254, #277, ... · o #281 bloqueia: #285, #286, #287, ...
crew-clerk           ‹ask r-106..115 → analyst · dep.relation›
crew-analyst         ‹answer› #254 blocks (0,94) · #285, #286, #287 blocked_by (0,91) ·
                              #277 unsure (0,58) · os outros none
crew-clerk           ‹ask r-116 → researcher · dep.relation #277›
crew-clerk           label → crew:deps:asked researcher                   ← a run termina

── deps researcher ─────────────────────────────────────────────────────────────────────
crew-researcher      ‹answer r-116› blocks: o #284 tira de internal/config os testes que o
                     R20 reescreve (internal/config/config_own_test.go)
crew-clerk           label → crew:deps:ready

── deps, de novo ───────────────────────────────────────────────────────────────────────
                     need_research: tudo respondido
                     record_deps: #254 → #281, #277 → #281, #281 → #285, #286, #287
crew-clerk           closing_comment (template): kept whole e o porquê, a sobreposição com
                     o PR #284, 5 links registrados com o porquê de cada um, shortlist usada,
                     0 comandos falharam
crew-clerk           label → crew:refinement:done
```

Os estados por onde o #281 passa:

```text
refinement:ready → split:ready → split:asked facilitator → split:ready (kept whole)
→ deps:ready → deps:asked researcher → deps:ready → refinement:done
```

Na versão com marcos, o mesmo caminho acontece todo em `crew:refinement:running`.
As linhas `label → …` viram eventos no journal, sem esperar o poll, e os pedidos e respostas do oráculo não vão para o issue.
O issue só recebe a mudança para `crew:refinement:done` e o comentário de fechamento.

Chega ao mesmo resultado da refinement real (kept whole; #254 e #277 bloqueiam; #285, #286 e #287 são bloqueados) com duas sessões curtas, uma do facilitador e uma do pesquisador, no lugar de uma sessão longa.

## O que o exercício mostra

- **O que precisa de LLM ficou claro.** Propor as partes de um split é escrita. Ler código para dizer se o #277 bloqueia o #281 pede evidência. O resto é listar, medir, validar, gravar links ou fazer uma pergunta estreita.
- **Não precisa de `start_answerer`, de rule de despacho, nem dos #255 e #256.** Com tudo reagindo a estados, quem responde reage como qualquer outra rule: o script pede, termina, e a sessão só responde. Na versão com um label por estado, o crew de hoje basta. Na versão com marcos, faltam os estados internos e o feed de comentários (ver [O que falta no crew](#o-que-falta-no-crew)). Com o chat como um segundo tracker, as rules não mudam: faltam vários trackers no config, o `move` entre trackers e o adapter `local` (ver [O que o crew precisa ganhar](#o-que-o-crew-precisa-ganhar)). Com comandos em comentários, faltam o watcher, o gatilho por comando e rules sem labels (ver [O que o crew precisa](#o-que-o-crew-precisa)).
- **Os scripts encolhem para lógica pura.** Listar, ler, comentar, mover e ligar issues são funções do tracker; o que sobra nos scripts é ranquear, medir, validar e escolher o veredito, de JSON para JSON (ver [Funções do tracker](#funções-do-tracker)).
- **Choca com o plano do #281.** O plano decide que a refinement vira uma skill `/cw-refine` numa sessão headless (R1 a R9, R17). Este modelo troca a skill por rules de código com pedidos. Os scripts e o veredito tipado que o #281 pede continuam, mas saem de dentro da skill e vão para as rules. Se esse for o caminho, o plano do #281 precisa voltar ao brainstorm antes do development.

## Em aberto

- **Quem tira o issue da espera do boss.** Com comandos em comentários, o watcher faz isso sozinho; o boss pode responder com `/crew answer`. Na versão com marcos, o feed de comentários faz o mesmo. Na versão com um label por estado, o boss ainda responde e move o label à mão, como o #255 prevê: o #255 deixa "o crew olhando o issue" fora do escopo.
- **O needs-human do R14.** Pelo R14, um par indecidível não para a run. Aqui ele vira uma pergunta ao boss que ninguém espera. Falta definir quem aplica o link quando o boss responder: uma rule pequena de acompanhamento, ou o próprio boss.
- **O oráculo para duplicatas.** O #281 deixa para o planejamento se o Jev deve fazer a shortlist de duplicatas. Aqui a shortlist é do script (ver [Candidatos a duplicata só com script](#candidatos-a-duplicata-só-com-script)), e o oráculo só julga o top 10 com `dup.kind`. O `AGENTS.md` pede medir antes em issues reais, então `dup.kind` começaria no estágio `shadow` do #203: o oráculo responde e o ledger guarda, enquanto o pesquisador decide.
- **O label do `split`.** Uma rota para um label que nenhuma rule pega tira o pai do crew, como o R18 do #281 pede. Falta escolher o nome.
