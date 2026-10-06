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
