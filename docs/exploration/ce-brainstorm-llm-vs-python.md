# ce-brainstorm: o que é LLM e o que é código

Rascunho. Para cada etapa da [visão geral do fluxo](ce-brainstorm-flow.md#visão-geral), separa o que precisa de uma LLM e o que um código determinístico, em Python por exemplo, resolveria.
A ideia é os dois trabalharem juntos: o código cuida do fluxo, e a LLM é chamada dentro dele.

## A regra

Tudo que tem regra fixa (ler arquivo, contar, seguir precedência, montar caminho, mostrar opções) vai para o código.
A LLM entra quando é preciso entender texto livre ou escrever texto.

Na prática, o Python seria a máquina de estados: os nós `ESTADO:` viram estados reais no código.
A cada passo que exige julgamento, o Python chama a LLM pedindo uma resposta num formato fixo (JSON) e decide a transição com essa resposta.

## Etapa por etapa

| Etapa | LLM indispensável | Python resolve |
|---|---|---|
| **1. Entrada e rota** | Classificar o pedido: software, não-software ou nenhum. Reconhecer se é um veredito sobre candidato externo. Classificar as decisões já trazidas como settled ou directive. Achar um formato pedido em linguagem natural ("quero em HTML"). Escrever a justificativa da oferta do `ce-pov`. | Tirar os tokens `mode:`, `output:` e `brainstorm_model:`, que são prefixos literais. Resolver o formato pela ordem config.local, config, md. Validar `docs_root`. Listar os planos com `artifact_contract: ce-unified-plan/v1`. |
| **2. Escopo** | Julgar se os requisitos estão claros, qual é o tier e se há mais de um resultado independente. Detectar os gatilhos: tema visual, você dizer que não conhece o domínio. | Criar as 5 tarefas, que são uma lista fixa. Aplicar "na dúvida, o tier mais pesado". |
| **3. Contexto** | O scout, que é um subagente LLM. O pressure test, que acha as lacunas do pedido. | `packs-resolve.py`, que já é Python. Criar a pasta temporária. Disparar o scout e o Slack em segundo plano. Decidir quais ramos rodam pelo tier e pelo caminho rápido. |
| **4. Diálogo** | Escrever cada pergunta e interpretar cada resposta. Detectar conflito com `CONCEPTS.md`, território desconhecido e decisão cara de desfazer. A checagem de integração. | O laço em si. Garantir uma pergunta por turno. Guardar a lista de lacunas e quais já foram sondadas. Verificar a condição de saída, desde que a LLM devolva cada critério como `true`/`false`. |
| **5. Abordagens** | Gerar as abordagens e recomendar uma. | Pular a etapa se é Lightweight ou caminho rápido. Escolher o modelo de elevação pelo token e pelo config. Escolher a rota: Agent, `elevation-dispatch.sh` ou inline. |
| **6. Síntese** | Escrever a síntese, com o problema, o que construir e os call outs. Interpretar sua resposta como confirmação, revisão ou "skill errada". Dizer qual decisão uma revisão tocou. O verificador de afirmações. | Escolher entre Path A e Path B, que só depende do tier e de quantas perguntas bloqueantes foram feitas (o Python fez as perguntas, então sabe quantas). Contar quantas vezes cada item foi revisado e disparar "Proceed ou Hold off?" na segunda vez. |
| **7. Escrita** | Decidir se o arquivo se justifica. Escrever o Product Contract. As checagens Consistent, Focused e Usable. Escolher os termos para o `CONCEPTS.md`. | Montar o caminho `YYYY-MM-DD-HHMM-type-topic-plan`, com reserva atômica e sufixo `-2`, `-3` em colisão. Escrever o frontmatter. A checagem Complete: nenhum `TBD`, seções obrigatórias presentes, IDs `R1`, `R2` contínuos, toda Outstanding Question classificada. |
| **8. Handoff** | Escrever as perguntas de Resolve Before Planning. Escrever o resumo final das decisões. | Quase tudo: o modo `return-to-caller`, as regras que escondem opções do menu (sem arquivo, sem `lfg`; HTML mostra "abrir no navegador"), mostrar o menu e chamar a skill escolhida. |

## Onde os dois se encontram

- **Classificações estreitas** (tipo de pedido, veredito ou não, tier) são julgamentos de uma pergunta só. Não precisam do modelo grande da sessão: dá para usar um modelo menor, ou o Jev, que o `AGENTS.md` deste repositório pede para considerar nesses casos. Antes, mediríamos com dados reais.
- **Condições de saída e checagens:** a LLM preenche um checklist estruturado, por exemplo `{ator: true, sucesso: false, ...}`, e o Python decide a transição. Assim a LLM não decide sozinha que "acabou".
- **Perguntas a você:** o Python mostra a pergunta e espera a resposta. A LLM só escreve o texto das opções.
- **Escrita:** a LLM gera o conteúdo das seções, e o Python monta o arquivo e valida a estrutura. Se a validação falha, o Python devolve o erro para a LLM corrigir.

## Conclusão

No diagrama 00, toda a máquina de estados (as setas e o que faz o fluxo seguir de um estado para outro) cabe em Python.
A LLM fica dentro das etapas, onde há texto para entender ou escrever.

Hoje a skill faz tudo isso dentro da LLM, guiada por prosa.
É por isso que ela precisa de frases como "Do not simplify the rule back to a single signal". Em código, essas regras viram um `if`.

## Um passo mais fundo: Etapa 7, escrita

Escolhi a Etapa 7 porque é a etapa em que LLM e código ficam mais equilibrados, e em que as regras que hoje estão em prosa viram código com mais facilidade.
As outras pendem para um lado: a Etapa 8 seria quase toda Python, e a Etapa 4 quase toda LLM.

| Passo | LLM | Python | Como conversam |
|---|---|---|---|
| **O arquivo se justifica?** | Responde a duas perguntas sobre a conversa: você pediu um arquivo? Há decisões que precisam de ID estável? | Escreve o arquivo se qualquer resposta for sim. | A LLM devolve `{user_asked_file, needs_stable_ids}` e o Python faz o `or`. |
| **Marca a tarefa 5 como pulada** | Nada. | Renomeia a tarefa para "Skipped: no doc warranted" e conclui. | Só código. |
| **Aplica os veredictos do verificador** | Reescreve cada afirmação refutada. | Lê o JSON do verificador e separa por veredito. Confere que cada afirmação "unverifiable" aparece em Dependencies / Assumptions. | O verificador devolve `{claim_id, verdict, evidence}`. O Python manda à LLM só as refutadas e depois confere a cobertura pelo `claim_id`. |
| **Lê a referência de renderização** | Nada. | Escolhe `markdown-rendering.md` ou `html-rendering.md` pelo formato resolvido na Etapa 1 e põe o texto no prompt. | Só código. |
| **Reserva o caminho** | Escolhe o `type` (`feat`, `fix`...) e o assunto em poucas palavras. | Resolve `docs_root`, gera `YYYY-MM-DD-HHMM` pelo relógio local e transforma o assunto em `topic` kebab-case. Cria o arquivo com abertura exclusiva (`O_EXCL`) e, em colisão, tenta `-2`, `-3`. Nunca sobrescreve. | A LLM dá `type` e assunto; o resto é código. |
| **Escreve Goal Capsule e Product Contract** | Escreve o conteúdo de cada seção, em prosa que passa pelo `ce-noslop`. | Monta o arquivo: frontmatter com os campos fixos; título com o sufixo " - Plan"; ordem das seções; numeração R1, R2... contínua entre grupos; IDs `A`, `F` e `AE`; `Governs R…` e a anotação `session-settled:` com o texto exato. | A LLM devolve as seções como dados (exemplo abaixo). O Python numera, liga e renderiza. |
| **Ready for Planning Check** | Consistent (contradições entre seções, requisito com dois resultados), Focused e Usable by planning. | Complete: nenhum `TBD` ou placeholder; seções obrigatórias presentes; toda Outstanding Question num dos dois baldes; marcador `work-relationships` se a Etapa 2 dividiu o pedido. Mais checagens mecânicas: todo `Governs` aponta para um R que existe; nenhum caminho absoluto; frase com mais de um parêntese. | O Python roda as checagens dele primeiro. Se passam, pede à LLM um parecer estruturado `{check, passed, problems[]}` para as três outras. |
| **Corrige no lugar** | Corrige. Diz se a correção mudaria comportamento ou escopo. | Repete as checagens até passarem. Pode pôr um limite de tentativas, que hoje a skill não tem. | Se a LLM marca `changes_scope: true`, o Python vai para a pergunta em vez de corrigir. |
| **Uma pergunta pontual** | Escreve a pergunta. | Mostra e espera a resposta. | Igual às outras perguntas. |
| **Atualiza o CONCEPTS.md** | Escolhe os termos que ficaram definidos e escreve as entradas. | Só roda se o arquivo existe. Depois da edição, confere pelo diff que nenhuma entrada foi apagada, porque só o `ce-compound-refresh` pode apagar. | A LLM edita e o Python valida o diff. |

### O contrato da escrita

O passo central troca "escreva o documento" por "devolva os dados". Por exemplo:

```json
{
  "title": "Notification Mute",
  "type": "feat",
  "topic": "notification mute",
  "goal_capsule": {"objective": "...", "authority": "...", "blockers": []},
  "summary": "...",
  "requirements": [
    {"group": "Mute", "text": "..."},
    {"group": "Mute", "text": "..."}
  ],
  "key_decisions": [
    {"decision": "...", "rationale": "...", "governs": [0, 1],
     "settled": {"class": "user-directed", "alternative": "...", "reason": "..."}}
  ],
  "outstanding_questions": [{"text": "...", "bucket": "deferred_to_planning"}],
  "extra_sections": [{"heading": "...", "markdown": "..."}]
}
```

A partir disso, o Python valida o schema (por exemplo, `class` só aceita `user-directed` ou `user-approved`), transforma `governs: [0, 1]` em `Governs R1, R2` e escreve o markdown ou o HTML.
O `extra_sections` existe porque a skill deixa a LLM criar seções que não estão no catálogo. Sem ele, o schema tiraria essa liberdade.

### O que o código garante e hoje é só pedido em prosa

- R-IDs contínuos e `Governs` que apontam para requisitos que existem.
- O texto exato de `session-settled:`, que outras skills procuram com grep.
- Nunca sobrescrever um plano existente.
- `CONCEPTS.md` sem entrada apagada.
- A checagem Complete, que hoje é a LLM conferindo o próprio trabalho.

O que continua só com a LLM é o que a skill chama de qualidade do texto: se o plano se contradiz, se cobre um trabalho só e se o `ce-plan` consegue planejar sem inventar.
