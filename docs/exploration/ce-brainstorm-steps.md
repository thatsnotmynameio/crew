# ce-brainstorm passo a passo

O que a skill `ce-brainstorm` do plugin Compound Engineering faz, da chamada até o handoff.
Lido do `SKILL.md`, das 20 referências e dos 4 scripts de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06).
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Lê a entrada**
   1. Usa o argumento como descrição da feature. Se não vier nenhum, pergunta o que você quer explorar e só continua com a resposta.
   2. Tira do texto os tokens de controle: `mode:return-to-caller` (o `lfg` passa esse), `output:md|html` e `brainstorm_model:<alias>`. Outros tokens `palavra:palavra`, como `feat:`, continuam no texto.
   3. Lê `interaction-rules.md`, que vale para a run inteira:
      - uma pergunta por turno;
      - múltipla escolha pelo `AskUserQuestion`;
      - pergunta aberta só quando ela é aberta de verdade;
      - só pergunta o que o repositório não responde;
      - recomenda só o que o objetivo exige;
      - oferece `ce-prototype` quando uma decisão é cara de desfazer.
   4. Lê `settled-decisions.md` e classifica as decisões que já vieram da conversa:
      - **settled**, com rótulo `user-directed` ou `user-approved`: nunca são perguntadas de novo;
      - **directive**: recebem um único questionamento;
      - **sem rótulo**: o resto.

2. **Fase 0.0: escolhe o formato de saída** (`output-mode.md`)
   - A ordem de prioridade é: pedido no prompt, depois preferência sua que já está no contexto, depois `brainstorm_output` em `config.local.yaml` e em seguida `config.yaml` (linhas comentadas são ignoradas), e por fim `md`.
   - A referência de renderização só é lida na fase 3.

3. **Fase 0.1: procura trabalho para retomar** (`phase-0.md`)
   1. Só roda dentro de um repositório git. Resolve `<root>` a partir de `docs_root`, lido só do `config.yaml`. O padrão é `docs`, e um valor inválido é erro, sem cair no padrão.
   2. Procura em `<root>/plans/` um plano recente sobre o mesmo tema, com `artifact_contract: ce-unified-plan/v1` e `product_contract_source: ce-brainstorm`, que ainda não tenha planejamento de implementação.
   3. Se encontrar, lê o plano e pergunta se continua dele ou começa do zero. Se continuar, resume o estado e atualiza o mesmo arquivo no mesmo formato.

4. **Fase 0.1b: classifica o domínio**
   - **Software**: segue para o passo 5.
   - **Não-software**: `universal-brainstorming.md` substitui as fases 0.2 a 4 (ver a seção "Rota não-software" no fim deste documento).
   - **Nenhum dos dois** (pergunta rápida, pergunta factual, tarefa de um passo): responde direto e encerra.
   - "Devemos adotar X?" conta como software e segue para a 0.1c.

5. **Fase 0.1c: decide se é caso de `ce-pov`** (`verdict-routing.md`)
   - Vale quando três coisas aparecem juntas: um candidato externo nomeado, a intenção de decidir se adota, e o julgamento feito contra este projeto.
   - Nesse caso, oferece por pergunta bloqueante passar para o `ce-pov`:
     - se você aceita, invoca o `ce-pov` e o brainstorm termina;
     - se recusa, o brainstorm segue e a oferta não volta.
   - A oferta também pode aparecer mais tarde, se o diálogo virar esse tipo de pergunta.

6. **Fase 0.2: verifica se os requisitos já estão claros**
   - Requisitos claros significam critérios de aceite, padrões a seguir, comportamento exato e escopo fechado.
   - Nesse caso a interação fica curta: pula o scan, o scout e o pressure test, mas a descoberta de packs ainda roda. Classifica o tier e vai direto para a 1.3 ou a 2.5.

7. **Fase 0.3: avalia o escopo**
   1. Classifica o trabalho como **Lightweight**, **Standard** ou **Deep**. Deep se divide em *feature* (estende um produto que já existe) e *product* (precisa definir o produto). Se o tier não está claro, faz uma pergunta. Se continuar incerto, escolhe o mais pesado.
   2. Lightweight termina num parágrafo no chat: sem arquivo, sem scout, sem abordagens e sem verificador. A exceção é quando surge uma decisão que precisa de ID estável, ou quando você pede um arquivo.
   3. **Checa se é um trabalho só.** Se o pedido tem mais de um resultado que dá para planejar separadamente:
      - propõe uma divisão;
      - pergunta qual parte este brainstorm cobre;
      - guarda as outras como contexto para a seção "How This Work Fits Together".
   4. **Gatilhos de alerta:**
      - se o tema é visual ou espacial, lê `visual-probes.md`;
      - se você disse que não conhece o domínio, lê `blindspot-pass.md`.

8. **Fase 0.4: cria a lista de tarefas** (só em Standard e Deep)
   1. Cria cinco tarefas pelo `TaskCreate`:
      1. Check what already exists
      2. Ask scoping questions
      3. Weigh approaches and recommend
      4. Confirm scope before writing
      5. Write the requirements plan
   2. Uma tarefa condicional (verificador, Slack, blindspot) só entra quando a condição acontece. Uma tarefa que não vai rodar é marcada como pulada, nunca como concluída.

9. **Fase 1.1: levanta o contexto** (`dialogue.md`)
   1. **Descobre os packs, em qualquer tier.** Roda `scripts/packs-resolve.py`, que:
      - lê `packs:` dos dois configs;
      - resolve fontes locais e git (o git é clonado num cache);
      - devolve JSON com `roots`, `warnings` e `errors`. Os erros aparecem uma vez. Sem packs, nada muda.
   2. **Lightweight:** procura o tema no código e vê se já existe algo parecido.
   3. **Standard e Deep:**
      1. Checa restrições sem sair da conversa: as instruções do projeto, o `STRATEGY.md` (ou `PRODUCT.md`/`VISION.md`) e o `CONCEPTS.md`, de onde tira o vocabulário canônico.
      2. Cria uma pasta temporária privada em `/tmp/compound-engineering-<uid>/ce-brainstorm/<run-id>`.
      3. Dispara em segundo plano um subagente "grounding scout", num modelo barato e com cerca de 20 leituras. Ele:
         - procura se algo parecido já existe, artefatos relevantes, exemplos próximos e o estado atual do que será tocado;
         - cita as regras dos packs que se aplicam;
         - escreve `grounding.md` com no máximo 150 linhas de citações com `file:line`;
         - devolve um resumo de 3 a 5 linhas.

         Enquanto ele roda, a skill já começa a perguntar.
   4. **Regras do levantamento:**
      - toda afirmação de que algo não existe é conferida no código; se não der para conferir, vira suposição;
      - detalhes de design técnico ficam para o planejamento.
   5. **Slack só se você pedir.** Se pediu e há ferramentas de Slack, dispara um subagente com `agents/slack-researcher.md`. Se há ferramentas mas você não pediu, só avisa que dá para usar.
   6. Se uma dúvida sobre como o sistema funciona mudaria o trabalho, chama o `ce-explain`.

10. **Fase 1.2: faz o pressure test** (`product-pressure-test.md`, só internamente, sem checklist para você)
    - **Lightweight:** isso resolve o problema real? Duplica algo que já existe? Há um enquadramento melhor que não custa nada?
    - **Standard:** procura quatro lacunas:
      - **evidência:** o que alguém já fez a respeito;
      - **especificidade:** quem exatamente é beneficiado;
      - **contrafactual:** qual é o contorno usado hoje;
      - **apego à solução:** qual é a menor versão que ainda entrega valor.

      Também pesa, por conta própria, qual é a jogada de maior alavancagem.
    - **Deep:** soma a pergunta "é remendo local ou move o sistema na direção certa?".
    - **Deep-product:** soma durabilidade, "que produto vizinho poderíamos construir por engano?" e "o que faria isso falhar?".

11. **Fase 1.3: conduz o diálogo**
    1. Primeiro pergunta o que você já está pensando. Depois vai do geral (problema, usuários, valor) para o específico (restrições, exclusões, casos de borda).
    2. Cada lacuna da 1.2 vira uma pergunta aberta separada, e todas são feitas antes da fase 2. A de apego à solução é a última.
    3. Se um termo seu contradiz o `CONCEPTS.md`, ou uma afirmação sua contradiz o código ou o dossier, aponta o conflito.
    4. **Oferta de blindspot.** Aparece se o tema foi marcado na 0.3, ou se duas respostas seguidas mostram que você não consegue avaliar a questão. Se você aceita:
       - monta um mapa de 3 a 7 decisões e riscos, baseado no dossier ou na web;
       - pergunta, com seleção múltipla, quais você quer percorrer;
       - percorre cada uma escolhida com um menu;
       - registra as não escolhidas como suposições com o valor padrão.
    5. **Oferta visual.** Se o tema é visual, antes da primeira decisão de forma, layout ou fluxo pergunta: rascunho visual ou descrição em texto? Com rascunho:
       - sobe `scripts/light-webserver.js` numa pasta temporária;
       - grava um HTML tosco em `screens/`;
       - mostra a URL;
       - recolhe sua reação pelo chat.
    6. Oferece `ce-prototype` quando uma decisão é cara de desfazer e não se resolve conversando.
    7. **Checagem de integração:** combina suas respostas e pergunta sobre consequências não óbvias da combinação.
    8. **Sai da fase quando** o ator, o resultado desejado, os limites de escopo e os critérios de sucesso (ou as suposições) estão definidos, todas as lacunas foram tratadas e não sobrou checagem pendente. Também sai se você pedir para avançar.

12. **Fase 2: gera as abordagens** (`approaches.md`, não roda em Lightweight)
    1. **Bake-off:** só se você pedir explicitamente. Nesse caso lê `bakeoff.md`, e o `ce-bakeoff` substitui a geração.
    2. **Elevação de modelo** (`reasoning-elevation.md`). Escolhe o modelo nesta ordem: o que você pediu, depois o token passado pela skill que chamou, depois `brainstorm_model` no config. Com um modelo escolhido, tenta três caminhos:
       1. `Agent` com o modelo trocado;
       2. senão, `scripts/elevation-dispatch.sh` rodando em segundo plano sob `scripts/peer-job-runner.py`: Claude CLI só com leitura, acompanhado com `wait` até sair o JSON de resultado;
       3. senão, gera no próprio modelo da sessão.

       Entrega o contexto como arquivos numa pasta temporária (dossier, decisões, convenções do projeto), confere o que voltou e escreve uma linha dizendo qual modelo, qual caminho e por quê.
    3. **Gera as opções:**
       - propõe 2 ou 3 abordagens, ou recomenda direto se só uma faz sentido;
       - inclui pelo menos um ângulo não óbvio (inversão, remover uma restrição, analogia);
       - aplica um teste contra respostas genéricas;
       - pode incluir uma alternativa mais ambiciosa.
    4. Cada abordagem traz descrição, prós e contras, riscos e quando ela serve, no nível do mecanismo e sem arquitetura.
    5. Mostra todas antes de recomendar. Recomenda a de menor escopo que cumpre o objetivo e diz se é reuso, extensão ou algo novo.

13. **Fase 2.5: apresenta a síntese** (`synthesis-summary.md`)
    1. Monta internamente um rascunho em três grupos: **Stated** (o que você disse), **Inferred** (o que ela supôs) e **Out of scope** (o que ficou de fora).
    2. **Path A** (Lightweight sem nenhuma pergunta bloqueante): mostra só "Proposing: …" e segue sem esperar.
    3. **Path B** (todos os outros casos):
       - mostra *The problem*, *What we're building*, *Carrying forward* e *Call outs*, escritos pelo `ce-noslop`;
       - termina pedindo confirmação com uma pergunta aberta, sem menu.
    4. Se você revisa, integra a mudança e mostra a síntese de novo. Só escreve depois de uma confirmação explícita. Se o mesmo item for revisado duas vezes, pergunta "Proceed" ou "Hold off".
    5. Se você indicar que esta não é a skill certa, ela para e sugere a outra.
    6. **Fase 2.6, junto com a pergunta de confirmação do Path B:** se o plano vai afirmar coisas verificáveis sobre o repositório, dispara um subagente verificador (cerca de 15 leituras). Ele devolve, para cada afirmação, *confirmed*, *refuted* ou *unverifiable*.

14. **Fase 3: escreve o plano** (`plan-write.md`, `brainstorm-sections.md`)
    1. **Decide se o arquivo se justifica.** Ele se justifica quando há decisões que precisam de ID estável, ou quando você pediu. Se não se justifica, o parágrafo do chat é o resultado e a tarefa 5 fica marcada como pulada.
    2. **Aplica o resultado do verificador:** corrige o que foi refutado e marca o não verificável como suposição.
    3. Lê `markdown-rendering.md` ou `html-rendering.md`. No HTML ainda procura um `DESIGN.md` para o estilo.
    4. **Reserva o caminho** `<root>/plans/YYYY-MM-DD-HHMM-<type>-<topic>-plan.<md|html>` de forma atômica. Em caso de colisão, acrescenta `-2`, `-3` antes da extensão.
    5. **Escreve o conteúdo:**
       - **Frontmatter:** `title` terminando em " - Plan", `type`, `date`, `topic`, `artifact_contract`, `product_contract_source` e `execution: code`.
       - **`## Goal Capsule`:** objetivo, autoridade e bloqueios.
       - **`## Product Contract`:** Summary e Requirements (com R-IDs, agrupados por assunto) sempre. As outras seções entram só quando acrescentam algo:
         - Problem Frame
         - Key Decisions (com `session-settled:` e `Governs R…`)
         - How This Work Fits Together
         - Actors
         - Key Flows
         - Visualizations
         - Acceptance Examples
         - Success Criteria
         - Scope Boundaries
         - Dependencies/Assumptions
         - Outstanding Questions (Resolve Before Planning / Deferred to Planning)
         - Sources, incluindo as citações `(pack: …)`
       - Não escreve Planning Contract nem Implementation Units: isso fica para o `ce-plan`.
    6. Escreve a prosa pelo `ce-noslop`, com regras de economia: a conclusão primeiro, cada regra escrita num lugar só, sem histórico empilhado.
    7. **Roda o Ready for Planning Check:** Complete, Consistent, Focused e Usable by planning.
       - Se uma checagem falha, corrige e roda de novo.
       - Se a correção mudaria comportamento ou escopo, faz uma pergunta antes.
    8. Informa o caminho absoluto do arquivo.
    9. Se existe `CONCEPTS.md`, acrescenta ou refina os termos que ficaram definidos.

15. **Fase 4: handoff** (`handoff.md`)
    1. **Com `mode:return-to-caller`:** em vez do menu, devolve `status`, `result_kind`, `artifact_path`, `brief`, `resolve_before_planning`, `key_decisions` e `grounding_path`.
    2. **Sem esse modo:**
       1. Se sobraram itens em *Resolve Before Planning*, pergunta um por vez. Você também pode pausar, ou mandar seguir mesmo assim, e aí cada item vira suposição ou fica para o planejamento.
       2. Monta o menu só com as opções que se aplicam:
          - **Create the implementation plan:** chama o `ce-plan` e passa o plano e o dossier;
          - **Ship it with `lfg`:** só se o arquivo existe e não sobrou pendência;
          - **Pressure-test** com `ce-doc-review`, **ou Prototype** com `ce-prototype`;
          - **Open in browser:** só para HTML;
          - **More clarifying questions:** volta para a 1.3.

          Usa `AskUserQuestion` se cabem 4 opções; senão, uma lista numerada.
       3. Executa a opção escolhida. Se você encerrar, mostra o resumo final: o caminho do plano, as decisões-chave e o próximo passo recomendado.

## Rota não-software

`universal-brainstorming.md` substitui as fases 0.2 a 4 por um brainstorm conversacional:

- escala Quick, Standard ou Full;
- gera ideias por ângulos variados e as compara;
- converge para uma recomendação.

Termina num resumo no chat e num menu com Create a plan, Save summary, Publish to Proof e Done.
Não grava nada em `plans/`.

## Skills que ele chama antes do handoff

- [`ce-pov`](ce-pov-steps.md): o veredito sobre adotar um candidato externo (passo 5).
- [`ce-explain`](ce-explain-steps.md): a explicação de um comportamento do sistema ou de um território que você não conhece (passos 9 e 11).
- [`ce-prototype`](ce-prototype-steps.md): o protótipo de uma decisão cara de desfazer (passos 11, 12 e 15).
- [`ce-bakeoff`](ce-bakeoff-steps.md): a competição entre abordagens, quando você pede (passo 12).
- [`ce-noslop`](ce-noslop-steps.md): a disciplina de escrita da síntese e do plano (passos 13 e 14).

As skills que ele só chama no handoff (`ce-plan`, `lfg`, `ce-doc-review` e `ce-proof`) não estão descritas aqui.
