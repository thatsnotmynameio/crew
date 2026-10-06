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

## Uma função de perguntas

Outro ângulo: uma função que recebe um estado e uma lista de perguntas sobre ele, e responde todas de uma vez.
Cada pergunta tem um tipo:

- **escolha:** a pergunta vem com as opções, e a resposta é uma delas, com um nível de confiança;
- **score:** a resposta é um número;
- **sim/não:** a resposta é sim ou não, com um nível de confiança.

Tanto o Python quanto a LLM podem chamar a função.

### Onde cabe

Nos losangos do fluxo: os pontos em que a skill julga um texto e escolhe uma saída de uma lista fechada.
Hoje a LLM principal faz esses julgamentos no meio da conversa, sem número nenhum.
Com a função, o Python passa o estado, recebe respostas tipadas e decide a transição.

| Etapa | Estado passado | Que julgamentos viram perguntas |
|---|---|---|
| **1. Entrada e rota** | O pedido e a conversa até ali | O domínio do pedido; se é um veredito sobre algo de fora; o formato pedido; a classe de cada decisão trazida; se um plano existente é do mesmo tema. |
| **2. Escopo** | O pedido e um resumo do repositório | Se os requisitos já estão claros; o tier; se há mais de um resultado independente; os gatilhos visual e de território desconhecido. |
| **3. Contexto** | O pedido | O pressure test: uma pergunta por lacuna do catálogo do tier. É o encaixe mais limpo, porque o catálogo já é uma lista fechada. |
| **4. Diálogo** | A conversa e a última resposta | Os gatilhos antes de cada pergunta; se a pessoa não sabe avaliar ou só não decidiu; cada critério da condição de saída. |
| **5. Abordagens** | As abordagens geradas e o objetivo | Quanto cada abordagem cumpre o objetivo e quanto escopo acrescenta. A skill manda recomendar a de menor escopo que cumpre o objetivo; com os scores, isso vira uma regra no Python. |
| **6. Síntese** | A síntese mostrada e a resposta da pessoa | O tipo da resposta; qual item da síntese uma revisão tocou, para o Python contar "mesmo item revisado 2 vezes"; se um trecho confirma uma afirmação, quando o estado já traz a evidência. |
| **7. Escrita** | A conversa, ou o plano escrito | Se o arquivo se justifica; as checagens Consistent, Focused e Usable; se um requisito pede duas coisas; se uma correção muda o escopo; se um termo ficou definido. |
| **8. Handoff** | O menu visível e a resposta da pessoa | Que opção foi escolhida; o que fazer com cada item de Resolve Before Planning. |

### Por que funciona nesses pontos

1. **São julgamentos estreitos com respostas fixas.** É o formato exato da função. A LLM grande deixa de fazer esse trabalho dentro da conversa.
2. **O lote aproveita o mesmo estado.** As perguntas de uma etapa leem o mesmo texto, então uma chamada só responde todas.
3. **A confiança substitui regras escritas em prosa.** A skill diz "se o tier continua incerto, escolha o mais pesado", "na dúvida, continue perguntando", "uma entrada que não bate com nenhuma opção é pedido de esclarecimento". Com a confiança, todas viram a mesma regra: abaixo do limite, pergunte à pessoa ou siga o caminho seguro.
4. **As respostas tipadas entram direto na máquina de estados em Python.** O Python não precisa interpretar texto.
5. **Dá para medir.** Cada pergunta tem um gabarito possível: os logs do `.crew/logs`, as issues e os planos já escritos. Os limites de confiança saem de dados separados para teste, que é o que o `AGENTS.md` pede para o Jev. Essa função tem o mesmo formato do Jev: uma pergunta estreita por vez, só o texto necessário no estado e uma probabilidade na resposta.

### Onde não cabe

- **Gerar texto.** Escrever as perguntas do diálogo, as abordagens, a síntese, o mapa do território e o plano. A função escolhe ou pontua; não escreve.
- **Procurar evidência.** O scout, o verificador e o pesquisador do Slack precisam abrir arquivos. A função só julga o que já está no estado.
- **Regras fixas.** Path A ou B, pular a Etapa 5, a visibilidade das opções do menu, a ordem de precedência do formato. Isso já é Python e não precisa de confiança.
- **A checagem de integração (1.3).** Dá para perguntar "existe consequência não óbvia?", mas a resposta útil é qual consequência, e isso é texto.

### Dois cuidados

- **Perguntas dependentes.** Algumas só fazem sentido depois de outras, como o tier, que só importa se o pedido é de software. Elas podem ir no mesmo lote, e o Python descarta as que não se aplicam. Mas se a resposta de uma muda o estado que a outra precisa ler, elas vão em lotes separados.
- **Confiança calibrada.** O número só serve para decidir se, quando ele diz 0,8, a resposta acerta umas 8 em 10. Isso precisa ser medido por pergunta antes de valer como regra.

## As perguntas, atômicas e simples

Uma pergunta por julgamento, escrita como uma pessoa perguntaria, sem o jargão da skill.

### A pergunta que abre cada pergunta à pessoa

**Preciso de um humano pra decidir?**
Ela cabe antes de cada paralelogramo laranja do fluxo, os pontos em que a skill pergunta algo à pessoa.
É a Interaction Rule 8 ("pergunte só o que o repositório não responde") virando código:

- **sim:** o Python faz a pergunta à pessoa;
- **não:** o Python resolve com o repositório ou com o padrão;
- **confiança baixa:** trata como sim;
- **run sem humano (pipeline):** sim quer dizer bloquear e devolver a pendência.

### Por etapa

**Etapa 1, entrada e rota**

- O pedido é sobre construir ou mudar software? *(sim/não)*
- O pedido é uma dúvida rápida que se responde direto? *(sim/não)*
- O pedido pergunta se vale adotar algo de fora? *(sim/não)*
- Em que formato o pedido quer o documento? *(escolha: markdown, HTML, não diz)*
- A pessoa escolheu isto depois de ver outra opção? *(sim/não, uma por decisão)*
- A pessoa só concordou com uma proposta? *(sim/não, uma por decisão)*
- Este plano fala do mesmo assunto do pedido? *(score, um por plano)*

**Etapa 2, escopo**

- O pedido já diz como deve funcionar? *(sim/não)*
- Qual é o tamanho do trabalho? *(escolha: pequeno, médio, grande)*
- O produto em volta já existe? *(sim/não)*
- Dá para entregar uma parte sem as outras? *(sim/não)*
- O assunto é visual? *(sim/não)*
- A pessoa disse que não conhece o assunto? *(sim/não)*

**Etapa 3, contexto** (uma pergunta por lacuna)

- O pedido mostra algo que alguém já fez para resolver isso? *(sim/não)*
- O pedido diz quem ganha com isso? *(sim/não)*
- O pedido diz o que se faz hoje sem isso? *(sim/não)*
- O pedido já chega com a solução pronta? *(sim/não)*

**Etapa 4, diálogo**

- Preciso de um humano pra decidir? *(sim/não, antes de cada pergunta)*
- A resposta contradiz o código? *(sim/não)*
- A resposta usa um termo com outro sentido no glossário? *(sim/não)*
- A pessoa sabe avaliar esta pergunta? *(sim/não)*
- A próxima decisão é sobre aparência? *(sim/não)*
- Errar esta decisão sai caro? *(sim/não)*
- Conversar resolve esta decisão? *(sim/não)*
- Sabemos quem vai usar? *(sim/não)*
- Sabemos o resultado esperado? *(sim/não)*
- Sabemos o que fica de fora? *(sim/não)*
- Sabemos como medir o sucesso? *(sim/não)*

"Errar esta decisão sai caro?" e "Conversar resolve esta decisão?" eram uma regra só na skill: a Rule 7, que manda oferecer o `ce-prototype`.
Separadas, o Python oferece o protótipo quando a primeira é sim e a segunda é não.

**Etapa 5, abordagens**

- Esta abordagem resolve o problema? *(score, uma por abordagem)*
- Quanto esta abordagem acrescenta além do pedido? *(score, uma por abordagem)*

**Etapa 6, síntese**

- O que a pessoa respondeu? *(escolha: confirmou, pediu mudança, quer outra skill)*
- Que item a mudança toca? *(escolha entre os itens da síntese)*
- Este trecho confirma esta afirmação? *(sim/não, uma por afirmação)*

**Etapa 7, escrita**

- A pessoa pediu um arquivo? *(sim/não)*
- Alguém vai precisar citar estas decisões depois? *(sim/não)*
- Uma seção contradiz outra? *(sim/não)*
- O plano cobre um trabalho só? *(sim/não)*
- Dá para planejar sem inventar comportamento? *(sim/não)*
- Este requisito pede duas coisas? *(sim/não, uma por requisito)*
- Esta correção muda o que vai ser construído? *(sim/não)*
- Este termo ficou bem definido? *(sim/não, um por termo)*

**Etapa 8, handoff**

- Que opção a pessoa escolheu? *(escolha entre as opções visíveis)*
- Esta pendência foi resolvida? *(sim/não, uma por pendência)*
