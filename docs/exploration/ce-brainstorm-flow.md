# ce-brainstorm como fluxo

O fluxo de [ce-brainstorm-steps.md](ce-brainstorm-steps.md) desenhado como uma máquina de estados com passos entre os estados.
Cobre da chamada até o menu de handoff. Quando o fluxo sai para outra skill, o diagrama só aponta para ela.
O fluxo está quebrado em uma visão geral e 8 etapas. Cada etapa começa no estado em que a anterior termina.
Cada diagrama também está renderizado em PNG em [`ce-brainstorm-flow/`](ce-brainstorm-flow/), um arquivo por etapa.

## Legenda

| Forma | Cor | Significa |
|---|---|---|
| Retângulo arredondado nas pontas, `ESTADO:` | azul | Estado: um ponto estável onde a skill chega depois de alguns passos |
| Retângulo | branco | Passo: uma ação da skill |
| Losango | amarelo | Decisão da skill, sem perguntar a você |
| Paralelogramo | laranja | Pergunta a você, e o fluxo espera a resposta |
| Retângulo com barras laterais | roxo | Outra skill: o fluxo vai para ela |
| Círculo | cinza | Fim da skill |
| Bandeira tracejada | cinza claro | Continua em outra etapa |

Setas tracejadas são trabalho em segundo plano: a skill dispara e segue sem esperar.

## Visão geral

Só os estados e as etapas que levam de um a outro.

```mermaid
%% ce-brainstorm: 00-visao-geral
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  START(("Chamada")) --> S_IN(["ESTADO: pedido recebido"])
  S_IN -->|"Etapa 1"| S_SW(["ESTADO: pedido de software"])
  S_IN -->|"Etapa 1"| END_DIRECT(("Responde direto"))
  S_IN -->|"Etapa 1"| END_UNIV(("Rota não-software"))
  S_IN -->|"Etapa 1"| POV[["ce-pov"]]
  S_SW -->|"Etapa 2"| S_TIER(["ESTADO: escopo classificado"])
  S_TIER -->|"Etapa 3"| S_DIALOG(["ESTADO: em diálogo"])
  S_TIER -->|"Etapa 3, caminho rápido<br/>sem diálogo"| S_READY
  S_DIALOG -->|"Etapa 4, em laço"| S_READY(["ESTADO: ideia entendida"])
  S_READY -->|"Etapa 5"| S_APPROACH(["ESTADO: abordagem escolhida"])
  S_READY -->|"Lightweight ou caminho rápido:<br/>pula a Etapa 5"| S_WAIT
  S_APPROACH -->|"Etapa 6"| S_WAIT(["ESTADO: aguardando confirmação"])
  S_WAIT -->|"Etapa 6: confirma"| S_CONFIRMED(["ESTADO: escopo confirmado"])
  S_WAIT -->|"Etapa 6: hold off"| S_DIALOG
  S_CONFIRMED -->|"Etapa 7"| S_PLAN(["ESTADO: plano escrito e validado"])
  S_CONFIRMED -->|"Etapa 7"| S_CHAT(["ESTADO: resultado no chat"])
  S_PLAN -->|"Etapa 8"| S_MENU(["ESTADO: handoff, menu apresentado"])
  S_CHAT -->|"Etapa 8"| S_MENU
  S_MENU -->|"mais perguntas"| S_DIALOG
  S_MENU --> NEXT[["ce-plan, lfg, ce-doc-review<br/>ou ce-prototype"]]
  S_MENU --> END_SUMMARY(("Resumo final"))

  class S_IN,S_SW,S_TIER,S_DIALOG,S_READY,S_APPROACH,S_WAIT,S_CONFIRMED,S_PLAN,S_CHAT,S_MENU state
  class POV,NEXT ext
  class START,END_DIRECT,END_UNIV,END_SUMMARY stop
```

## Etapa 1: entrada e rota

Da chamada até saber que é um pedido de software (fase 0.0 a 0.1c).

```mermaid
%% ce-brainstorm: 01-entrada-e-rota
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  START(("Chamada")) --> D_ARG{"Veio descrição<br/>da feature?"}
  D_ARG -- não --> Q_ARG[/"O que você quer explorar?"/]
  Q_ARG --> D_ARG
  D_ARG -- sim --> A_TOK["Remove tokens de controle<br/>mode:, output:, brainstorm_model:"]
  A_TOK --> A_RULES["Lê interaction-rules.md<br/>e settled-decisions.md"]
  A_RULES --> A_SETTLED["Classifica decisões já trazidas:<br/>settled, directive, sem rótulo"]
  A_SETTLED --> S_IN(["ESTADO: pedido recebido"])
  S_IN --> A_FMT["0.0 Resolve o formato:<br/>prompt, preferência, config, md"]
  A_FMT --> D_REPO{"Repositório git<br/>e root válido?"}
  D_REPO -- não --> D_DOMAIN
  D_REPO -- sim --> A_SCAN["0.1 Procura plano ce-brainstorm<br/>do tema em root/plans"]
  A_SCAN --> D_FOUND{"Achou?"}
  D_FOUND -- não --> D_DOMAIN
  D_FOUND -- sim --> Q_RESUME[/"Continuar deste plano<br/>ou começar do zero?"/]
  Q_RESUME -- "do zero" --> D_DOMAIN
  Q_RESUME -- continuar --> A_RESUME["Lê o plano e resume o estado:<br/>mesmo arquivo, mesmo formato"]
  A_RESUME --> D_DOMAIN
  D_DOMAIN{"0.1b Que tipo<br/>de pedido?"}
  D_DOMAIN -- nenhum --> END_DIRECT(("Responde direto<br/>e encerra"))
  D_DOMAIN -- "não-software" --> END_UNIV(("Segue universal-brainstorming.md<br/>e encerra por lá"))
  D_DOMAIN -- software --> D_VERDICT{"0.1c É veredito sobre<br/>candidato externo?"}
  D_VERDICT -- sim --> Q_POV[/"Quer um veredito do ce-pov?"/]
  Q_POV -- sim --> POV[["ce-pov"]]
  POV --> END_POV(("ce-pov assume<br/>a sessão"))
  Q_POV -- não --> S_SW
  D_VERDICT -- não --> S_SW(["ESTADO: pedido de software"])
  S_SW --> GO>"Etapa 2: escopo"]

  class S_IN,S_SW state
  class D_ARG,D_REPO,D_FOUND,D_DOMAIN,D_VERDICT decision
  class Q_ARG,Q_RESUME,Q_POV ask
  class POV ext
  class START,END_DIRECT,END_UNIV,END_POV stop
  class GO go
```

## Etapa 2: escopo

Do pedido de software até o escopo classificado (fase 0.2 a 0.4).

```mermaid
%% ce-brainstorm: 02-escopo
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_SW(["ESTADO: pedido de software"]) --> D_CLEAR{"0.2 Requisitos<br/>já claros?"}
  D_CLEAR -- sim --> A_FAST["Marca caminho rápido:<br/>sem scan, scout e pressure test"]
  A_FAST --> A_TIER
  D_CLEAR -- não --> A_TIER["0.3 Classifica o tier:<br/>Lightweight, Standard ou Deep"]
  A_TIER --> D_TIER{"Tier claro?"}
  D_TIER -- não --> Q_TIER[/"Uma pergunta de escopo"/]
  Q_TIER --> A_HEAVY["Se continua incerto,<br/>escolhe o tier mais pesado"]
  A_HEAVY --> D_SPLIT
  D_TIER -- sim --> D_SPLIT{"Mais de um resultado<br/>independente?"}
  D_SPLIT -- sim --> Q_SPLIT[/"Propõe divisão:<br/>qual área este brainstorm cobre?"/]
  Q_SPLIT --> A_CTX["As outras áreas viram contexto"]
  A_CTX --> A_TRIP
  D_SPLIT -- não --> A_TRIP["Arma os gatilhos:<br/>visual, território desconhecido"]
  A_TRIP --> S_TIER(["ESTADO: escopo classificado"])
  S_TIER --> D_TASKS{"Standard<br/>ou Deep?"}
  D_TASKS -- sim --> A_TASKS["0.4 Cria as 5 tarefas"]
  A_TASKS --> GO
  D_TASKS -- não --> GO>"Etapa 3: contexto"]

  class S_SW,S_TIER state
  class D_CLEAR,D_TIER,D_SPLIT,D_TASKS decision
  class Q_TIER,Q_SPLIT ask
  class GO go
```

## Etapa 3: contexto

Do escopo classificado até o diálogo (fase 1.1 e 1.2).

```mermaid
%% ce-brainstorm: 03-contexto
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_TIER(["ESTADO: escopo classificado"]) --> A_PACKS["1.1 Descobre packs<br/>packs-resolve.py"]
  A_PACKS --> D_FASTP{"Caminho rápido?"}
  D_FASTP -- "sim, tudo já foi dito" --> S_READY(["ESTADO: ideia entendida"])
  S_READY --> GO5>"Etapa 5: abordagens"]
  D_FASTP -- "sim, ainda falta algo" --> S_DIALOG
  D_FASTP -- não --> D_DEPTH{"Tier?"}
  D_DEPTH -- Lightweight --> A_LSEARCH["Procura o tema no código"]
  A_LSEARCH --> A_PRESS
  D_DEPTH -- "Standard ou Deep" --> A_CONSTR["Lê STRATEGY.md e CONCEPTS.md"]
  A_CONSTR --> A_SCRATCH["Cria pasta temporária"]
  A_SCRATCH -.-> A_SCOUT["Grounding scout<br/>grava grounding.md"]
  A_SCRATCH --> D_SLACK{"Você pediu Slack<br/>e há ferramentas?"}
  D_SLACK -.->|sim| A_SLACK["Subagente slack-researcher"]
  D_SLACK -- "segue sem esperar" --> A_PRESS["1.2 Pressure test interno:<br/>anota as lacunas do pedido"]
  A_PRESS --> S_DIALOG(["ESTADO: em diálogo"])
  S_DIALOG --> GO4>"Etapa 4: diálogo"]

  class S_TIER,S_READY,S_DIALOG state
  class D_FASTP,D_DEPTH,D_SLACK decision
  class GO4,GO5 go
```

## Etapa 4: diálogo

O laço de perguntas até a ideia entendida (fase 1.3).

```mermaid
%% ce-brainstorm: 04-dialogo
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_DIALOG(["ESTADO: em diálogo"]) --> D_GATE{"1.3 Antes da próxima pergunta:<br/>algum gatilho vale?"}
  D_GATE -- conflito --> Q_CONFLICT[/"Aponta conflito com<br/>CONCEPTS.md ou código"/]
  D_GATE -- "território desconhecido" --> Q_BLIND[/"Mapear o território primeiro?"/]
  Q_BLIND -- sim --> A_MAP["Mapa de 3 a 7<br/>decisões e riscos"]
  A_MAP --> Q_WALK[/"Quais quer percorrer?"/]
  D_GATE -- "decisão visual" --> Q_VISUAL[/"Rascunho visual ou texto?"/]
  Q_VISUAL -- visual --> A_SKETCH["Sobe light-webserver.js<br/>e mostra o rascunho"]
  D_GATE -- "cara de desfazer" --> Q_PROTO[/"Prototipar com ce-prototype?"/]
  Q_PROTO -- sim --> PROTO[["ce-prototype"]]
  D_GATE -- "dúvida sobre o sistema" --> EXPLAIN[["ce-explain"]]
  D_GATE -- "virou veredito" --> Q_POV[/"Quer um veredito do ce-pov?"/]
  Q_POV -- sim --> POV[["ce-pov"]]
  POV --> END_POV(("ce-pov assume<br/>a sessão"))
  D_GATE -- nenhum --> Q_NEXT[/"Próxima pergunta, uma por turno:<br/>lacuna da 1.2 ou estreitamento"/]

  Q_CONFLICT --> D_EXIT
  Q_BLIND -- não --> D_EXIT
  Q_WALK --> D_EXIT
  Q_VISUAL -- texto --> D_EXIT
  A_SKETCH --> D_EXIT
  Q_PROTO -- não --> D_EXIT
  PROTO --> D_EXIT
  EXPLAIN --> D_EXIT
  Q_POV -- não --> D_EXIT
  Q_NEXT --> D_EXIT

  D_EXIT{"Condição de saída?<br/>ator, resultado, limites,<br/>sucesso, lacunas tratadas"}
  D_EXIT -- não --> S_DIALOG
  D_EXIT -- sim --> D_INTEG{"Combinação de respostas<br/>com consequência não óbvia?"}
  D_INTEG -- sim --> Q_INTEG[/"Pergunta sobre a consequência"/]
  Q_INTEG --> S_DIALOG
  D_INTEG -- não --> S_READY(["ESTADO: ideia entendida"])
  S_READY --> GO>"Etapa 5: abordagens"]

  class S_DIALOG,S_READY state
  class D_GATE,D_EXIT,D_INTEG decision
  class Q_CONFLICT,Q_BLIND,Q_WALK,Q_VISUAL,Q_PROTO,Q_POV,Q_NEXT,Q_INTEG ask
  class PROTO,EXPLAIN,POV ext
  class END_POV stop
  class GO go
```

## Etapa 5: abordagens

Da ideia entendida até a abordagem escolhida (fase 2).

```mermaid
%% ce-brainstorm: 05-abordagens
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_READY(["ESTADO: ideia entendida"]) --> D_SKIP{"Lightweight ou<br/>caminho rápido?"}
  D_SKIP -- sim --> GO6A>"Etapa 6: síntese,<br/>sem abordagens"]
  D_SKIP -- não --> D_BAKE{"Você pediu<br/>bake-off?"}
  D_BAKE -- sim --> BAKE[["ce-bakeoff"]]
  BAKE --> A_PRESENT
  D_BAKE -- não --> D_ELEV{"Há modelo de elevação?<br/>pedido, token ou config"}
  D_ELEV -- sim --> A_ELEV["Gera no modelo escolhido:<br/>Agent, elevation-dispatch.sh<br/>ou inline"]
  A_ELEV --> A_GEN
  D_ELEV -- não --> A_GEN["Gera 2 ou 3 abordagens,<br/>uma com ângulo não óbvio"]
  A_GEN --> A_PRESENT["Mostra todas,<br/>depois recomenda"]
  A_PRESENT --> D_VIABLE{"Mais de uma viável?"}
  D_VIABLE -- sim --> Q_APPROACH[/"Qual abordagem?"/]
  Q_APPROACH --> S_APPROACH
  D_VIABLE -- não --> S_APPROACH(["ESTADO: abordagem escolhida"])
  S_APPROACH --> GO6>"Etapa 6: síntese"]

  class S_READY,S_APPROACH state
  class D_SKIP,D_BAKE,D_ELEV,D_VIABLE decision
  class Q_APPROACH ask
  class BAKE ext
  class GO6,GO6A go
```

## Etapa 6: síntese

Da abordagem escolhida até o escopo confirmado (fase 2.5 e 2.6).

```mermaid
%% ce-brainstorm: 06-sintese
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_APPROACH(["ESTADO: abordagem escolhida"]) --> A_DRAFT
  S_READY(["ESTADO: ideia entendida<br/>sem Etapa 5"]) --> A_DRAFT["2.5 Rascunho interno:<br/>Stated, Inferred, Out of scope"]
  A_DRAFT --> D_PATH{"Lightweight e nenhuma<br/>pergunta bloqueante?"}
  D_PATH -- "sim, Path A" --> A_PATHA["Mostra 'Proposing: ...'<br/>e segue sem esperar"]
  A_PATHA --> S_CONFIRMED
  D_PATH -- "não, Path B" --> A_PATHB["Mostra a síntese via ce-noslop:<br/>problema, o que construir, call outs"]
  A_PATHB --> D_CLAIMS{"O plano vai afirmar<br/>coisas do repositório?"}
  D_CLAIMS -.->|sim| A_VERIFY["2.6 Verificador de afirmações<br/>em segundo plano"]
  D_CLAIMS --> Q_CONFIRM[/"Confirma ou diz o que mudar"/]
  Q_CONFIRM --> S_WAIT(["ESTADO: aguardando confirmação"])
  S_WAIT -- confirma --> S_CONFIRMED(["ESTADO: escopo confirmado"])
  S_WAIT -- revisa --> D_TWICE{"Mesmo item<br/>revisado 2 vezes?"}
  D_TWICE -- não --> A_INTEGRATE["Integra a revisão"]
  A_INTEGRATE --> Q_CONFIRM
  D_TWICE -- sim --> Q_PROCEED[/"Proceed ou Hold off?"/]
  Q_PROCEED -- proceed --> S_CONFIRMED
  Q_PROCEED -- "hold off" --> GO4>"volta à Etapa 4: em diálogo"]
  S_WAIT -- "skill errada" --> END_REDIRECT(("Para e sugere<br/>outra skill"))
  S_CONFIRMED --> GO7>"Etapa 7: escrita"]

  class S_APPROACH,S_READY,S_WAIT,S_CONFIRMED state
  class D_PATH,D_CLAIMS,D_TWICE decision
  class Q_CONFIRM,Q_PROCEED ask
  class END_REDIRECT stop
  class GO4,GO7 go
```

## Etapa 7: escrita

Do escopo confirmado até o plano escrito ou o resultado no chat (fase 3).

```mermaid
%% ce-brainstorm: 07-escrita
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_CONFIRMED(["ESTADO: escopo confirmado"]) --> D_DOC{"3. O arquivo<br/>se justifica?"}
  D_DOC -- não --> A_SKIP5["Marca a tarefa 5 como pulada"]
  A_SKIP5 --> S_CHAT(["ESTADO: resultado no chat"])
  D_DOC -- sim --> A_APPLY["Aplica os veredictos<br/>do verificador"]
  A_APPLY --> A_RENDER["Lê markdown-rendering.md<br/>ou html-rendering.md"]
  A_RENDER --> A_PATH["Reserva root/plans/<br/>YYYY-MM-DD-HHMM-type-topic-plan"]
  A_PATH --> A_WRITE["Escreve Goal Capsule e<br/>Product Contract via ce-noslop"]
  A_WRITE --> D_CHECK{"Ready for Planning<br/>Check passa?"}
  D_CHECK -- "não, correção mantém a intenção" --> A_FIX["Corrige no lugar"]
  D_CHECK -- "não, mudaria escopo" --> Q_FIX[/"Uma pergunta pontual"/]
  Q_FIX --> A_FIX
  A_FIX --> D_CHECK
  D_CHECK -- sim --> S_PLAN(["ESTADO: plano escrito e validado"])
  S_CHAT --> A_VOCAB["Atualiza CONCEPTS.md,<br/>se existir"]
  S_PLAN --> A_VOCAB
  A_VOCAB --> GO>"Etapa 8: handoff"]

  class S_CONFIRMED,S_CHAT,S_PLAN state
  class D_DOC,D_CHECK decision
  class Q_FIX ask
  class GO go
```

## Etapa 8: handoff

Do resultado até o menu e as skills seguintes (fase 4).

```mermaid
%% ce-brainstorm: 08-handoff
flowchart TD
  classDef state fill:#dbeafe,stroke:#1d4ed8,stroke-width:3px,color:#0b1f4d,font-weight:bold
  classDef decision fill:#fef9c3,stroke:#a16207,color:#422006
  classDef ask fill:#ffedd5,stroke:#c2410c,color:#431407
  classDef ext fill:#ede9fe,stroke:#6d28d9,stroke-width:2px,color:#2e1065
  classDef stop fill:#e5e7eb,stroke:#374151,color:#111827
  classDef go fill:#f1f5f9,stroke:#64748b,stroke-dasharray:4 3,color:#0f172a

  S_IN(["ESTADO: plano escrito<br/>ou resultado no chat"]) --> D_CALLER{"mode:return-to-caller?"}
  D_CALLER -- sim --> END_CALLER(("Devolve status, artifact_path<br/>e key_decisions à skill chamadora"))
  D_CALLER -- não --> D_RBP{"Resolve Before Planning<br/>tem itens?"}
  D_RBP -- sim --> Q_RBP[/"Um item por vez:<br/>resolver, pausar ou seguir"/]
  Q_RBP -- resolver --> D_RBP
  Q_RBP -- "seguir mesmo assim" --> A_ASSUME["Itens viram suposições<br/>ou Deferred to Planning"]
  A_ASSUME --> S_MENU
  Q_RBP -- pausar --> S_PAUSED(["ESTADO: brainstorm pausado"])
  D_RBP -- não --> S_MENU(["ESTADO: handoff, menu apresentado"])
  S_PAUSED --> Q_MENU
  S_MENU --> Q_MENU[/"O que fazer agora?"/]
  Q_MENU -- "criar plano, recomendado" --> PLAN[["ce-plan"]]
  Q_MENU -- "ship com lfg" --> LFG[["lfg"]]
  Q_MENU -- "pressure-test" --> DOCREVIEW[["ce-doc-review"]]
  DOCREVIEW --> S_MENU
  Q_MENU -- prototipar --> PROTO[["ce-prototype"]]
  Q_MENU -- "abrir no navegador" --> S_MENU
  Q_MENU -- "mais perguntas" --> GO4>"volta à Etapa 4: em diálogo"]
  Q_MENU -- encerrar --> END_SUMMARY(("Resumo final"))

  class S_IN,S_PAUSED,S_MENU state
  class D_CALLER,D_RBP decision
  class Q_RBP,Q_MENU ask
  class PLAN,LFG,DOCREVIEW,PROTO ext
  class END_CALLER,END_SUMMARY stop
  class GO4 go
```

## Notas

- **Caminho rápido (0.2).** Com requisitos já claros, a skill pula o scan, o scout e o pressure test. A descoberta de packs ainda roda, e ela vai para o diálogo ou direto para a síntese.
- **Lightweight.** Pula as abordagens (fase 2), o verificador (2.6) e quase sempre o arquivo (fase 3). Termina com o resultado no chat.
- **Gatilhos da 1.3.** São checados antes de cada pergunta do diálogo, não uma vez só. O de território desconhecido e o visual só valem se a 0.3 os armou, ou se o diálogo os revelou.
- **`ce-pov` aceito.** O `ce-pov` assume a sessão e o brainstorm termina.
- **`ce-prototype` durante o diálogo.** Volta para o brainstorm com a decisão tomada. No menu do handoff, ele substitui a opção de pressure-test quando sobra uma pergunta que só um protótipo resolve.
- **Opções do menu.** Algumas ficam escondidas. `ce-plan` e `lfg` somem enquanto há itens em Resolve Before Planning. `lfg` e pressure-test só aparecem com um arquivo escrito. "Abrir no navegador" só aparece com HTML.
