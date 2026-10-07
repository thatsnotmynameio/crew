# ce-brainstorm como rules do crew

Rascunho para evoluir. Esboça o [nível 4](ce-brainstorm-llm-vs-python.md#do-mais-fraco-ao-mais-forte) aplicado ao `ce-brainstorm`: o crew é o motor do fluxo de [`ce-brainstorm.flow.yaml`](ce-brainstorm.flow.yaml).
Só nomeia as ações; nenhuma está escrita.

O modelo é o de uma equipe remota totalmente assíncrona.
Todo o trabalho acontece em comentários de issues e PRs no GitHub: perguntas, respostas, rascunhos, revisões.
As rules do crew são só código: decidem o próximo passo e, quando precisam de algo que código não faz, pedem no issue a um papel da equipe.
Nenhuma rule chama um modelo, nem o Jev, diretamente.

Usa o que o #220 traz: rules como sequência de ações com rotas por veredito (#254), e perguntas no tracker respondidas por pessoas e agentes de confiança (#255).
[`refinement-crew-rules.md`](refinement-crew-rules.md) exercita o mesmo modelo num fluxo mais curto, a refinement do #281.

## Os atores

| Ator | Quem é | O que faz | Por trás |
|---|---|---|---|
| boss | um code owner, pessoa | decide o produto, responde perguntas abertas, revisa o PR | — |
| crew | o motor, em Go; escreve como `crew-clerk[bot]` | pega o issue, roda as ações, segue as rotas, move labels | — |
| script | as ações shell das rules, código do repositório | decide os pedidos, espera respostas, escolhe o veredito, monta e valida | — |
| analista (o oráculo) | `crew-analyst[bot]` | responde perguntas estreitas com resposta fixa: sim/não, escolha, score | o judge (#203), com o Jev |
| facilitador | `crew-product-manager[bot]` | escreve: a próxima pergunta, abordagens, síntese, o contrato do plano | sessão do Claude (LLM) |
| pesquisador | `crew-researcher[bot]` | lê o repositório: contexto, evidência, verificação, explicação | sessão do Claude (LLM), modelo barato, só leitura |

O analista, o facilitador e o pesquisador são os papéis a quem o script pede.
O script só conhece o papel e o nome do pedido.
Quem responde por trás do papel é configuração: trocar o Jev por outro provedor, ou o modelo do pesquisador, não muda nenhuma rule.

## As ações de cada ator

### boss

| Ação | Onde | O que faz |
|---|---|---|
| `open_idea` | issue, ou `/cw-create-issue` numa sessão | abre a ideia com `crew:brainstorm:ready` |
| `answer` | issue | responde um pedido endereçado a ele: o número da opção ou texto livre |
| `answer_via_session` | sessão local do Claude Code | dita a resposta e a sessão posta como ele. A sessão não põe o marcador do crew: um comentário com o marcador nunca conta como resposta (R43 do #255) |
| `override` | issue | responde um pedido feito a um bot, no lugar dele. Vale, porque é code owner (R37) |
| `confirm` | issue | reage 👍 a uma resposta do analista quando a pergunta está no estágio `confirm` |
| `review_pr` | PR | aprova, pede mudanças ancoradas na linha, resolve threads |
| `resume` | issue | devolve o label para `ready`. Só existe enquanto o crew não retoma sozinho ao chegar a resposta |

### crew

| Ação | O que faz |
|---|---|
| `take` | pega o issue no `ready` e move para `running` |
| `run_action` | roda as ações da rule em ordem |
| `route` | veredito → rota → move o label |
| `resume_run` | retoma na ação que parou (R22 do #254) |
| `start_answerer` | **novo.** Vê um pedido aberto para um bot seu e aciona o provedor dele: o judge, para o analista; uma sessão, para o facilitador e o pesquisador |
| `crew issues N ask` | **novo.** Posta um pedido com o marcador |
| `crew issues N answers` | **novo.** Devolve só as respostas que contam (R37 a R43 do #255) |
| `crew issues N thread` | **novo.** Reconstrói o estado a partir dos marcadores (`thread_fold`) |

### script

| Ação | O que faz |
|---|---|
| `request_<passo>` | decide quais pedidos fazer e chama `crew issues N ask` |
| `await_<passo>` | chama `crew issues N answers` e sai com 3 (`waiting`) quando falta resposta |
| `pick_<passo>` | lê o thread e sai com o código do veredito |
| `set_tier`, `recommend`, `find_plans`, `packs_resolve` | as regras fixas do flow.yaml |
| `check_grounding`, `validate_contract`, `render_plan`, `check_complete` | validam e montam a saída das LLMs |
| `open_synthesis_pr`, `merge_plan_pr`, `write_issue_body` | o PR e o corpo do issue |

Nenhum script chama um modelo.
O que é genérico (pedir, esperar, ler o thread) fica nos comandos do crew; o que é do brainstorm (quais perguntas cada passo faz, as regras dos `pick_*`) fica em scripts curtos do repositório e no banco do judge.

### analista

| Ação | O que faz |
|---|---|
| `answer_typed` | recebe a pergunta nomeada e os comentários citados, procura a pergunta no banco do judge, consulta o Jev e posta o valor com a probabilidade |
| `escalate` | abaixo da banda, responde "não sei" e reendereça ao boss |
| `record` | grava no ledger a pergunta, a resposta e, depois, o desfecho que vem da decisão do boss |

O estágio da pergunta no #203 decide o peso da resposta: em `shadow`, o boss decide e o analista só registra; em `confirm`, o boss confirma; em `act`, a resposta do analista vale sozinha.

### facilitador

| Ação | Saída |
|---|---|
| `write_carried_decisions` | as decisões que o pedido já traz |
| `write_split` | a proposta de divisão |
| `write_turn_question` | o texto da próxima pergunta, para a lacuna que o script escolheu |
| `write_integration_question` | a consequência não óbvia, se houver |
| `write_approaches` | 2 ou 3 abordagens |
| `write_synthesis` | a síntese e as claims |
| `revise_synthesis` | push no PR, a partir dos comentários de revisão |
| `write_contract`, `fix_plan` | o contrato do plano e as correções |

Cada ação é uma sessão curta para um pedido só.
A saída é um comentário no schema do pedido, ou um push no PR.
O facilitador não move label nem escolhe o próximo passo.

### pesquisador

| Ação | Saída |
|---|---|
| `ground` | `grounding.md` com file:line |
| `answer_from_repo` | a resposta, pelo repositório, a uma pergunta que ia para o boss; ou `null`, e a escalada ao boss |
| `verify_claims` | um veredito por claim |
| `explain_system` | como o sistema faz X |

## Onde está a LLM

A LLM não aparece nas rules.
Ela fica atrás do facilitador e do pesquisador, e só trabalha quando alguém faz um pedido a um deles:

```text
1. script (bs-dialogue)   posta no issue:
                          ‹ask dialogue-9 → facilitator · turn_question · write›
                          @crew-product-manager Escreva a pergunta sobre a lacuna "contrafactual".

2. crew (no poll)         vê um pedido aberto para o seu bot crew-product-manager
                          → inicia uma sessão do Claude como esse bot
                            prompt = o pedido + os comentários que ele cita

3. sessão do Claude       escreve a pergunta, posta a resposta no issue e termina
                          ‹answer dialogue-9› "Hoje, quando alguém quer segurar uma rule...?"

4. script (bs-dialogue)   o await vê a resposta, o pick lê e o fluxo segue
```

O código decide quando a LLM entra e o que ela recebe.
A LLM só escreve a resposta daquele pedido: não sabe em que etapa está nem o que vem depois.

O passo 2 é o `start_answerer`, que o crew ainda não tem: hoje ele só inicia uma sessão que seja ação de uma rule.
Há dois caminhos:

- **A. O crew responde os pedidos endereçados aos seus bots.** Uma capacidade nova, e o desenho deste rascunho.
- **B. Com o crew de hoje, a sessão vira uma ação da rule, logo depois do pedido:**

  ```yaml
  actions:
    - request-turn-text            # script: posta o pedido
    - agent: product-manager       # LLM: "responda os pedidos abertos endereçados a você"
      name: answer
    - await-turn-text              # script: confere que a resposta chegou
  ```

  Funciona sem mudar o crew. A sessão roda mesmo quando não há pedido para ela, porque o crew não pula uma ação; ela termina logo, mas já foi aberta.

## Quando a LLM pergunta

De dois jeitos:

1. **A pergunta que o fluxo quer fazer.** O script escolhe a lacuna, o facilitador escreve o texto, e o script posta e encaminha. A LLM escreve; quem pergunta é o código.
2. **A LLM trava no meio do próprio pedido.** Ao escrever a síntese, o facilitador nota duas respostas do boss que se contradizem. Ele pergunta pelo mecanismo do #255: posta a pergunta com o marcador, espera até o limite e termina com `waiting`. A pergunta vira um pedido como qualquer outro, passa antes pelo pesquisador e fica no thread, onde o fluxo a vê.

## O protocolo no tracker

### Pedido

Um comentário de quem pede, endereçado a um papel por menção, com um marcador oculto que o código lê:

```text
<!-- crew:ask id=bs-intake-3 to=analyst name=request.software type=yes_no about=c123 -->
@crew-analyst O pedido é sobre construir ou mudar software? (sim/não)
```

- `name` é a pergunta nomeada. As perguntas estreitas vivem no banco do judge (#203), não na rule.
- `about` aponta para os comentários que formam o estado da pergunta. A pergunta leva só o texto necessário.
- `type` é `yes_no`, `choice`, `score`, `open`, `evidence` ou `write`. Os três primeiros vão para o analista, `evidence` para o pesquisador, `write` para o facilitador e `open` para o boss.

### Resposta

Um comentário com `<!-- crew:answer q=bs-intake-3 value=... p=0.93 -->` e o texto legível.
Só conta a resposta de quem pode responder, pelo R37 ao R43 do #255: os code owners e os bots da lista de quem responde. O comentário de um estranho nunca conta.

### Escalada

Quem não sabe responder passa a pergunta adiante no próprio thread, como alguém de uma equipe faria:

- **O analista, abaixo da banda de confiança:** responde `value=unsure` e reendereça ao boss. É o `on_uncertain: human` do flow.yaml virando conversa. Os estágios do #203 encaixam aqui: em `shadow` o analista responde e o boss decide; em `confirm` o boss confirma com uma reação; em `act` a resposta do analista vale sozinha.
- **Pergunta para o boss passa antes pelo pesquisador.** O pesquisador responde pelo repositório (`{answer, evidence[]}`) ou devolve `answer: null`, e aí o código reendereça ao boss. É a Interaction Rule 8, "só pergunta o que o repositório não responde", e o `human_gate` do flow.yaml.

### Resposta do boss

O boss responde com o número da opção ou com texto livre.
O código lê o número.
Um texto que o código não consegue ler vira uma nova pergunta ao analista: "Que opção esta resposta escolhe?".

### O thread é o estado

As vars do flow.yaml não ficam num arquivo: `thread_fold` reconstrói o estado lendo os marcadores do issue, em ordem.
Os artefatos (decisões trazidas, `grounding.md`, abordagens, síntese) também são comentários, com `<!-- crew:artifact kind=... -->`.
O thread é ao mesmo tempo o estado, o histórico de cada transição e o dado para calibrar o analista depois.

## Do flow.yaml para o crew

| No flow.yaml | No crew |
|---|---|
| estado com `checkpoint: true` | o label `ready` de uma rule |
| etapa | uma rule |
| `run` | ação shell `[code]` |
| `ask` | pedido ao analista |
| `llm` | pedido ao facilitador |
| `human` | pedido ao boss, passando antes pelo pesquisador |
| `spawn` / `await` | pedido ao pesquisador; `await` espera a resposta |
| `routes` / `when` | uma ação `pick_*` `[code]` lê o thread e sai com um código, que vira veredito e rota |
| `vars` | `thread_fold` sobre os marcadores do issue |

No caminho A, nenhuma rule do brainstorm tem uma sessão de agente: as sessões existem só atrás do pesquisador e do facilitador.

## Três ações que se repetem

- `request_<passo>` `[code]`: posta os pedidos do passo com `crew issues N ask`. É idempotente pelo `id` do marcador: um pedido que já está no thread não é postado de novo.
- `await` `[code]`: espera, com `crew issues N answers`, as respostas a todos os pedidos abertos da rule, até o prazo do papel mais lento entre eles. Com todas as respostas, passa. Sem elas, sai com o veredito `waiting`, que leva ao label `crew:<rule>:waiting`.
- `pick_<passo>` `[code]`: lê o thread com `crew issues N thread` e decide o veredito.

Prazos de `await` por papel, a calibrar:

| Papel | Espera dentro da run | Sem resposta |
|---|---|---|
| analista | até 1 min | `waiting`: o provedor caiu |
| pesquisador, facilitador | até 20 min | `waiting` |
| boss | não espera | `waiting` logo depois de perguntar |

Toda ação é idempotente sobre o thread, porque uma rule sem sessão retoma na própria ação que parou (R22 do #254).
Um laço é uma rota para o próprio `ready`, com um contador no thread que leva a `needs person` passado o limite.

## Onde entram os PRs

A síntese e o plano são documentos, e numa equipe remota um documento se revisa num PR.

- **A síntese (Etapa 6) vira um PR de rascunho** com o arquivo do plano em `docs/plans/`. O boss revisa como revisa código.
- **Aprovar o PR é a confirmação.** O código lê o estado da revisão. Não precisa classificar a resposta.
- **Um comentário de revisão fica ancorado numa linha**, então a seção que ele toca é dado do GitHub. O "Que item a mudança toca?" sai sem pergunta, e contar "mesmo item revisado 2 vezes" vira contar rodadas de `changes requested` na mesma seção.
- **As threads de revisão têm resposta e "resolver".** Uma pergunta pontual da Etapa 7 vira uma thread no trecho que ela afeta, e uma thread resolvida conta como respondida.
- **O merge leva o plano para `docs/plans/`.** O corpo do issue fica com o Goal Capsule e o link. Isso resolve o limite de 65.536 caracteres de uma vez, e uma mudança no `CONCEPTS.md` entra no mesmo PR.

## As rules

Legenda: `[code]` shell determinístico; `→ papel: nome` é um pedido postado por um `request_*`.
Toda rule também tem `failed` (relatório e `crew:<rule>:failed`) e `waiting` (`crew:<rule>:waiting`). O bloco abaixo só mostra essas rotas onde elas importam.

```yaml
bs-intake:                         # ready: crew:brainstorm:ready   (Etapa 1)
  - request_carried                → facilitador: carried_decisions        (write)
  - await
  - request_decision_labels        → analista: decision.saw_option, decision.just_agreed
                                     (um par por decisão)
  - find_plans                     [code]  docs/plans, ce-unified-plan/v1, sem ce-plan ainda
  - request_triage                 → analista: request.software, request.quick, request.verdict,
                                     plan.same_topic (um por plano)
  - await
  - request_resume                 → boss: continuar do plano ou do zero
                                     (só se algum same_topic passou da banda)
  - await
  - pick_intake_exit               [code]  → quick | not_software | verdict
  routes: passed → bs-scope · quick, not_software, verdict → needs person

bs-scope:                          # Etapa 2
  - request_scope                  → analista: scope.clear, scope.size, scope.product,
                                     scope.parts, scope.visual, scope.unfamiliar
  - await
  - request_scope_question         → boss: a pergunta de escopo (só se scope.size voltou unsure)
  - request_split                  → facilitador: split (só se scope.parts)
  - await
  - request_split_area             → boss: qual área este brainstorm cobre
  - await
  - set_tier                       [code]  tier, fast_path; ainda incerto → o mais pesado
  - packs_resolve                  [code]  o packs-resolve.py que já existe
  - pick_scope_exit                [code]  → light | fast_done | fast_dialogue
  routes: passed → bs-context · light → bs-context · fast_done → bs-synthesis
          fast_dialogue → bs-dialogue

bs-context:                        # Etapa 3
  - read_constraints               [code]  AGENTS.md, STRATEGY.md, CONCEPTS.md → artefato
  - request_grounding              → pesquisador: grounding (evidence; só se não for light)
  - request_pressure_test          → analista: catálogo do tier (gap.*)
  - await
  - check_grounding                [code]  ≤150 linhas, todo file:line existe
  routes: passed → bs-dialogue

bs-dialogue:                       # Etapa 4: um turno por run
  - request_triggers               → analista: turn.contradicts_code, turn.other_meaning,
                                     turn.can_evaluate, turn.looks, turn.costly,
                                     turn.talkable, turn.verdict, turn.unknown_system
  - await
  - pick_turn                      [code]  turn_kind e o gap da vez
  - request_explain                → pesquisador: explain (só se turn.unknown_system)
  - request_turn_text              → facilitador: a pergunta do turn_kind (write)
  - await
  - request_turn                   → boss, passando antes pelo pesquisador
  - await
  - request_exit                   → analista: exit.who, exit.outcome, exit.limits,
                                     exit.success, exit.move_on
  - await
  - request_integration            → facilitador: consequência não óbvia (só se fechou)
  - await
  - pick_dialogue_exit             [code]  → again | understood | understood_light |
                                     turn_cap | prototype | pov
  routes: again → bs-dialogue · understood → bs-approaches · understood_light → bs-synthesis
          turn_cap, prototype, pov → needs person

bs-approaches:                     # Etapa 5
  - request_approaches             → facilitador: approaches (write, modelo elevado)
  - await
  - request_scores                 → analista: approach.solves, approach.extra (por abordagem)
  - await
  - recommend                      [code]  menor extra com solves na banda
  - request_choice                 → boss: qual abordagem (só se mais de uma viável)
  - await
  - pick_approaches_exit           [code]  → bakeoff (se o pedido trouxe)
  routes: passed → bs-synthesis · bakeoff → needs person

bs-synthesis:                      # Etapa 6
  - request_synthesis              → facilitador: synthesis + claims (write)
  - await
  - open_synthesis_pr              [code]  PR de rascunho com o plano; idempotente
  - request_claims                 → pesquisador: um veredito por claim (evidence)
  - await_review                   [code]  estado da revisão do PR; sem revisão → waiting
  - pick_synthesis_exit            [code]  approved | changes (seções pelas âncoras) |
                                     changes_twice → pergunta "Proceed ou Hold off?"
  routes: passed → bs-write · changes → bs-synthesis · hold → bs-dialogue

bs-write:                          # Etapa 7
  - request_contract               → facilitador: contract.json, com as claims refutadas corrigidas
  - await
  - validate_contract              [code]  schema, class, governs, unverifiable em Assumptions
  - render_plan                    [code]  frontmatter, R1..Rn, Governs, session-settled
  - check_complete                 [code]  TBD, seções, IDs, perguntas classificadas
  - request_quality                → analista: plan.contradicts, plan.one_unit, plan.plannable,
                                     req.two_things (por requisito)
  - await
  - pick_write_exit                [code]  → fix | scope_question | fix_cap
  - push_plan                      [code]  o plano no PR
  routes: passed → bs-handoff · fix → bs-write · scope_question → bs-write (thread no PR)
          fix_cap → needs person

bs-handoff:                        # Etapa 8
  - request_open_items             → boss: uma thread no PR por item de Resolve Before Planning
  - await_review                   [code]  threads resolvidas e PR aprovado
  - merge_plan_pr                  [code]
  - write_issue_body               [code]  corpo antigo num comentário, Goal Capsule e link
  routes: passed → crew:brainstorm:done
```

Os ramos visual, `ce-prototype`, `ce-pov`, `ce-bakeoff`, Slack, não-software e "dúvida rápida" vão para `needs person` nesta primeira versão.

## Um turno do diálogo, passo a passo

```text
 #  ator          ação                          aparece no issue
 1  crew          take                          label → crew:bs-dialogue:running
 2  script        request_triggers              ‹ask d-1..8 → analyst› 8 perguntas de gatilho
 3  crew          start_answerer(analyst)       —
 4  analista      answer_typed + record         ‹answer d-1..8› todas "não"
 5  script        await_triggers                — (respostas chegaram em segundos)
 6  script        pick_turn                     — escolhe: pergunta sobre a lacuna "contrafactual"
 7  script        request_turn_text             ‹ask d-9 → facilitator›
 8  crew          start_answerer(facilitator)   sessão do Claude como crew-product-manager
 9  facilitador   write_turn_question           ‹answer d-9› "Hoje, quando alguém quer segurar uma rule...?"
10  script        request_turn                  ‹ask d-10 → researcher› essa pergunta, antes do boss
11  crew          start_answerer(researcher)    sessão só leitura
12  pesquisador   answer_from_repo              ‹answer d-10 null› "o repositório não responde. @boss?"
13  script        await_turn                    — sai com waiting
14  crew          route(waiting)                label → crew:bs-dialogue:waiting
     ⋯ duas horas ⋯
15  boss          answer                        "Paro o crew e tiro a rule do config.local.yaml."
16  boss          resume                        label → crew:bs-dialogue:ready (até o crew retomar sozinho)
17  crew          take + resume_run             retoma no await_turn (R22)
18  script        await_turn                    — a resposta conta (code owner, depois da pergunta)
19  script        request_exit                  ‹ask d-11..15 → analyst›
20  analista      answer_typed                  ‹answer d-11..15› who sim · outcome sim · limits não ...
21  script        pick_dialogue_exit            — veredito again (limits ainda aberto)
22  crew          route(again)                  label → crew:bs-dialogue:ready → próximo turno
```

Nesse turno, a LLM entrou duas vezes (9 e 12), o analista duas (4 e 20) e o boss uma (15 e 16).
O resto foi script e crew.
Os passos 3, 8 e 11 são o `start_answerer`; no caminho B, viram ações de sessão dentro da própria rule.

## O que falta no crew

1. **Quem responde os bots.** Alguém precisa ver os pedidos endereçados ao analista, ao pesquisador e ao facilitador, e responder como esses bots. Além do caminho B de [Onde está a LLM](#onde-está-a-llm), há dois:
   - **O crew responde.** A cada poll, o crew procura pedidos abertos endereçados aos seus bots e responde pelo provedor de cada papel: o port do judge para o analista, uma sessão para o pesquisador e o facilitador. Não precisa mudar o label do issue. É o caminho que encaixa com a equipe assíncrona, e é uma capacidade nova do crew.
   - **Passar a bola com o label.** Cada papel é uma rule, e o issue vai para o `ready` dela. Esbarra em duas coisas do crew de hoje. Uma issue com mais de um label de estado é pulada (`skipped` em `internal/core/scheduler.go`), então o issue sai da rule do brainstorm. E a rota de volta precisa de um alvo variável, que exigiria uma rule de despacho. Cada passe também custa até um poll (300 s).
2. **Retomar quando chega a resposta.** Pelo #255, quem responde devolve o issue para `ready`. Os bots podem fazer isso ao responder. O boss teria de responder e mover o label. Numa equipe assíncrona, a resposta em si deveria retomar a rule. O #255 deixa "o crew olhando o issue" fora do escopo.
3. **Pedir a partir de shell.** O `wait:` e o filtro de quem responde valem para sessões (#255). O `await` precisa do mesmo filtro, e é preciso decidir com que bot um pedido postado por shell sai. Os comandos `crew issues N ask|answers|thread` resolveriam as duas coisas. O "Deferred for later" do #255 já cita um comando parecido, `crew sessions <id>`, que devolveria só as respostas que contam.
4. **O captain, como caixa de entrada.** Como motor, o captain do #213 não faz sentido: o motor já é o crew (label, rule, rota, journal, R22), e um captain que decide o próximo passo seria um segundo lugar de verdade, pediria uma sessão longa puxando tarefas (o nível 3, que devolve o controle à LLM) e ficaria invisível no issue. Como caixa de entrada do bot, faz: `crew sessions <id> tasks next` devolveria o próximo pedido aberto endereçado ao bot daquela sessão, e uma sessão acordada por três pedidos responderia os três com o mesmo contexto. Hoje nada chama `crew sessions`, então mudar o papel dele não custa nada.
5. **Bandas do analista.** Cada pergunta nomeada precisa de uma banda medida em dados separados para teste. O estágio `shadow` do #203 gera esses dados com o próprio uso: o boss decide e o ledger guarda a resposta do analista ao lado.

## Decisões em aberto

1. **Quem responde os bots:** o crew (`start_answerer`), a sessão como ação da rule (caminho B, que já funciona) ou rules com passe de label.
2. **Um pedido por comentário ou em lote.** Para o analista, um comentário com o lote e um com as respostas deixa o thread mais limpo. Para o boss, um pedido por comentário deixa responder cada um separado, mas os comentários de issue não têm threads como os de revisão de PR.
3. **O PR desde a síntese.** Abrir o PR na Etapa 6 dá a revisão ancorada e resolve o limite do corpo do issue, mas põe todo plano de brainstorm em `docs/plans/` no `main`.
4. **Uma pergunta por turno ao boss.** É a regra da skill numa sessão ao vivo. Num thread assíncrono, juntar os gaps independentes do pressure test numa rodada corta voltas, mas muda a regra.
