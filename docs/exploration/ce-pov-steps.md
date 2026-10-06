# ce-pov passo a passo

O que a skill `ce-pov` do plugin Compound Engineering faz, da chamada até a entrega.
Lido do `SKILL.md`, das 9 referências (incluindo `pov-schema.json`), das 4 personas em `references/agents/` e dos 2 scripts de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06).
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Lê a entrada** (`SKILL.md`)
   1. O assunto é o que veio com a chamada, no prompt ou na conversa: uma pergunta, um documento ou um conjunto de abordagens, com `cross-check` opcional. A chamada também pode vir sem argumento.
   2. A skill fica só em leitura enquanto forma e reconcilia a posição. Ela não implementa nada.
   3. Não entrevista você para descobrir o que avaliar. Tira a pergunta do pedido e da conversa e procura os fatos que dá para verificar.
   4. Se falta uma informação que mudaria a recomendação e ela não acha, devolve **Blocked — missing context**, dizendo o que falta, por que importa e o que resolveria. Quem chamou decide o próximo passo. Vale igual se você chamou direto ou se foi outro agente: não existe modo não interativo separado.
   5. Procura um pedido de painel (um "summons"): um pedido explícito para consultar ou reconciliar outros modelos, como "panel", "cross-check", `oracle` ou nomes de peers. Recusar um painel ("solo POV, do not cross-check") ou só citar um painel antigo não conta. Se o pedido aparece num canal e outro canal só o parafraseia, ele continua valendo.
   6. Usa 2026 como ano corrente para julgar se uma fonte é recente.
   7. Se foi chamada por outro workflow, devolve o resultado para ele e não acrescenta ofertas de follow-up nem de painel. Um pedido explícito de `oracle` ou de peer nomeado roda o painel mesmo assim.

2. **Resolve o `<root>`** (`SKILL.md`)
   1. Só resolve na primeira vez que compõe um caminho `<root>/`. Ler `<root>/solutions/` já conta.
   2. Lê `docs_root` só de `<repo-root>/.compound-engineering/config.yaml`, nunca de `config.local.yaml`. Sem valor, `<root>` é `docs`.
   3. Valida o valor: precisa ser um diretório relativo ao repositório que, com symlinks resolvidos, fica dentro do repositório, não é a raiz e não está sob `.git/`. Um valor inválido para com erro citando `docs_root` e o valor, sem cair em `docs`.
   4. Cria `<root>` se não existe e passa aos scouts o caminho já resolvido, nunca o config.
   5. Fora de um repositório git não há `<root>`: a busca de decisões anteriores usa os ADRs e documentos de design locais.

3. **Fase 0: enquadra a pergunta** (`intake.md`)
   1. **Formato de saída:** por padrão não escreve documento nenhum. Um relatório só sai se você pedir, na fase 4.
   2. **Cold ou warm:** se a skill entrou no meio de uma sessão ("weigh in", "give me your POV on this"), é uma invocação warm e lê `invocation.md` (ver a seção "Rota warm" no fim deste documento). Se você abriu a sessão com uma pergunta explícita, é cold e roda o método inteiro.
   3. **Orienta-se no que recebeu**, com uma leitura ou um fetch só:
      - **link solto:** busca uma vez para saber o que é e dar nome a ele. Se não consegue buscar (sem ferramenta web, paywall), devolve a informação que falta;
      - **tópico ou nome solto:** reconhece pelo próprio conhecimento e só pesquisa uma vez se não souber o que é;
      - **caminho de documento:** lê os títulos para entender o propósito, sem revisar ainda;
      - **conjunto de abordagens:** identifica as opções que já estão na mesa, sem inventar outras;
      - **texto colado:** lê.
   4. **Classifica a intenção:**
      - **Adopt:** usar uma capacidade nova, sem incumbente;
      - **Migrate / replace:** trocar um incumbente por isso;
      - **Compare:** como se compara com o que existe, sem troca implícita;
      - **Exposure:** um CVE, uma deprecação ou uma mudança de ecossistema é problema nosso?;
      - **Document-take:** a visão de conjunto de um documento, não uma lista de achados;
      - **Approach-set:** qual das abordagens fornecidas serve ao projeto, ou se qualquer uma serve;
      - **Explainer:** o que você quer é entender. Encaminha para o `ce-explain` com a pergunta e o uso pretendido, e para aqui.
   5. **Resolve o enquadramento.** Atalhos como "the approach" ou "these options" vêm da conversa quando só um referente cabe. Com assunto e intenção claros, não pede confirmação. Se leituras diferentes mudariam o julgamento, investiga o que dá e devolve **Blocked — missing context**. Pode assumir algo que tenha suporte, desde que não invente um compromisso de produto nem escolha por você uma preferência em aberto.
   6. **Confere se a skill é a certa** (`boundaries.md`, lido quando o encaixe é duvidoso). Se a intenção é de outra skill, encerra o intake encaminhando, sem veredito:
      - explicação neutra: `ce-explain`;
      - "review this doc" ou "find the issues": `ce-doc-review`;
      - opções inventadas de um campo aberto: `ce-ideate`;
      - definir o escopo de uma ideia já escolhida: `ce-brainstorm`;
      - como construir algo decidido: `ce-plan`, só com a autorização da fase 4;
      - corrigir um comportamento quebrado: `ce-debug`;
      - tese de produto ou direção da empresa: `ce-strategy`;
      - opções brutas que precisam ser desenvolvidas: `ce-bakeoff`.

      Se a skill de destino não está disponível, informa isso. Sem nenhum material local para comparar, o pedido está fora de escopo e a skill devolve o contexto de projeto que falta.
   7. **Aplica a válvula de seleção.** Uma pergunta do tipo "o que usamos para auth?" só fica aqui se o campo é limitado (umas cinco opções reais ou menos) e os critérios são conhecíveis. Senão, devolve um **Hold** que diz o que falta e nomeia quem resolve:
      - brief definido, mas os candidatos precisam ser desenvolvidos: `ce-bakeoff`;
      - campo aberto demais: `ce-ideate`, e devolve a exigência de uma lista curta;
      - critérios pouco claros: `ce-brainstorm`, e devolve os critérios que faltam.
   8. **Classifica a reversibilidade**, a partir de sinais do projeto:
      - **Tier 1, porta de duas mãos:** uma dependência, uma regra de lint, uma config;
      - **Tier 2, mão única mas contida:** um banco de dados, uma API interna ou uma migração cujo impacto fica dentro do código;
      - **Tier 3, mão única e alto risco:** segurança, jurídico, privacidade, API pública ou migração de dados irreversível.

      O tier define a profundidade da investigação, não o formato da resposta. Tier 1 faz um levantamento combinado. Tier 2 soma os três scouts e uma passada de alternativas. Tier 3 soma pesquisa externa funda e busca de precedentes.

4. **Fase 1: levanta as evidências** (`grounding.md`)
   1. **Tiers de modelo**, nunca um nome fixo:
      - **extraction** (o scout de projeto e o de precedentes): o modelo mais barato capaz, quando o harness permite escolher; senão herda;
      - **generation** (o pesquisador externo): um modelo intermediário, nas mesmas condições;
      - **ceiling:** o julgamento final fica com o agente que roda o `ce-pov` e nunca é despachado.

      Se a plataforma não deixa escolher o modelo por agente, todos herdam, e o custo fica controlado pelos orçamentos de leitura e pelo número de scouts.
   2. **Cria a pasta temporária** uma vez: `/tmp/compound-engineering-<uid>/ce-pov/<8 hex>`, com modo 700 (ou sob `$TMPDIR` se `/tmp` não serve). Recusa symlink ou dono diferente e mostra o caminho, que reusa na run inteira.
   3. **Delimita o candidato** com as instruções do projeto que já estão no contexto. Se isso não basta, faz uma sondagem dirigida na raiz ou no workspace.
   4. **Escolhe o caminho:**
      - se os fatos de que o veredito depende já estão localizados, confirma com leituras limitadas da fonte autoritativa, sem scouts;
      - se o levantamento está difuso ou ruidoso, despacha os scouts;
      - nos dois casos, a busca de decisões anteriores (`<root>/solutions/`, ADRs, documentos de design) é obrigatória;
      - uma afirmação da conversa é só uma pista a conferir.
   5. **Prepara o prompt de cada scout.** Um subagente novo não herda a conversa. Por isso cada um recebe a pergunta enquadrada (assunto e intenção), o incumbente nomeado, o tier e a pasta temporária, e o pesquisador externo recebe também os links que você passou. A skill preenche os placeholders `{subject}` e `{scratch-dir}` das personas.
   6. **Tier 1:** dois subagentes. O scout de projeto roda com orçamento apertado, e a busca de decisões anteriores dele é a única checagem de precedente. O pesquisador externo também roda. O scout de precedentes não roda.
   7. **Tier 2 e 3:** três subagentes em paralelo.
      1. **Scout de projeto** (`agents/project-grounding-scout.md`, extraction, cerca de 15 leituras, menos no Tier 1). Ele:
         - acha o incumbente pelo manifesto, pelo lockfile ou pelo código; ou, se é adoção nova, registra as buscas que vieram vazias, para a ausência ser verificada;
         - checa compatibilidade: versão de linguagem e runtime, dependências e a licença do candidato contra a do projeto;
         - mede o custo: quantos pontos de chamada usam o incumbente, ou onde o candidato entraria;
         - vê se existe uma abstração concorrente ou uma convenção a seguir;
         - coleta sinais de dor (`TODO`, `FIXME`, `HACK`, `workaround`) como citação, não como fato;
         - procura decisões anteriores em `<root>/solutions/`, ADRs e documentos de design;
         - escreve `<scratch>/project-grounding.md`, com no máximo 120 linhas de citações com `file:line`, agrupadas em Incumbent, Compatibility, Migration cost, Convention fit e Incumbent pain;
         - devolve um resumo de 3 a 5 linhas dizendo se o piso de projeto parece atingível, mais o caminho do arquivo.
      2. **Scout de precedentes e atividade** (`agents/precedent-activity-scout.md`, extraction, cerca de 15 leituras). Ele:
         - lê sempre, primeiro, o registro local de decisões (`<root>/solutions/`, ADRs, documentos de design);
         - depois, se há um tracker acessível (connector ou MCP, `gh`, uma API documentada), procura issues e PRs pelo tema e pelo nome do incumbente. Lê descrições e comentários, nunca diffs. Um PR fechado sem merge ("tried X, backed it out") é um achado valioso;
         - sem tracker, anota que pulou essa parte e segue;
         - escreve `<scratch>/precedent-activity.md`, com no máximo 120 linhas com identificador e data, agrupadas em Precedent e Incumbent pain & exposure. "No prior stance found" é um achado válido;
         - devolve um resumo de 3 a 5 linhas e o caminho.
      3. **Pesquisador externo** (`agents/external-evidence-researcher.md`, generation). Só roda com ferramentas web. Ele:
         - sem busca nem fetch, informa "external research unavailable" e para;
         - pesquisa maturidade e trajetória, armadilhas (postmortems, não só a página do fornecedor), a realidade de migração e o contrafactual (o custo de ficar no incumbente e as alternativas);
         - aceita uma afirmação só se o texto da fonte a sustenta. Prefere duas fontes independentes e marca o que tem uma só. Desconta afirmações com mais de uns 12 meses;
         - no Tier 3 recebe um brief mais fundo: mais fontes, mais leituras e duas fontes obrigatórias para tudo o que sustenta o veredito;
         - escreve `<scratch>/external-evidence.md`, com no máximo 120 linhas com URL e data, agrupadas em Maturity & trajectory, Pitfalls, Migration reality e Counterfactual, cada afirmação marcada `[verified: <url>]` ou `[single-source]`;
         - devolve um resumo de 3 a 5 linhas e o caminho.
   8. **Só pula o que nada alcança.** O scout de projeto e a parte local do scout de precedentes são leituras de arquivo e sempre rodam. Uma ferramenta que falta nunca bloqueia: fica registrada e baixa a confiança, ou derruba o piso externo na fase 2.
   9. **Se um despacho é rejeitado:** verifica se o agente chegou a subir. Se foi argumento errado, corrige uma vez. Se falta capacidade, deixa na fila. Se ainda falha, levanta aquela evidência sem sair da conversa e baixa a confiança declarada.
   10. **Chama o `ce-explain`** quando o julgamento depende de explicar um comportamento ou uma razão de design ainda não resolvidos. Passa a pergunta, o escopo e a decisão que ela informa. Trata o que volta como evidência a avaliar, não como autoridade. Sem `ce-explain`, levanta a evidência direto ou diz o que falta.
   11. **Separa os fatos pela origem:**
       - contam como evidência: fatos observados no projeto e fatos externos verificados;
       - não contam: afirmações da conversa e suposições não confirmadas, até que um scout ou uma leitura da fonte as confirme.

       Lê os dossiês pelo caminho só quando precisa, sem puxar o conteúdo todo para o contexto.

5. **Fase 2: verifica as evidências** (`method.md`)
   1. Segue quatro passos: Frame (fase 0), Precedent (fase 1, lido antes de dar nota), Verify (esta fase) e Point of view (fase 3).
   2. **Postura cética, em todos os passos:** procura evidência contrária e nomeia as alternativas reais, incluindo "manter o incumbente" e "não fazer nada". "No", "Reject" e "Not-our-problem" são resultados legítimos.
   3. Uma chamada a uma função prova que a chamada acontece, não como a função se comporta. Garantias se verificam na implementação ou nos testes. Sem essas fontes, a garantia fica como desconhecida, e um propósito inferido leva rótulo de inferência.
   4. **Pergunta de adoção externa: dois pisos independentes**, que funcionam como passa ou não passa:
      - **piso de projeto:** passa com um incumbente nomeado e pelo menos um ponto de contato concreto (`file:line`, dependência, issue, PR, trecho de doc); ou com a ausência verificada de incumbente e um ponto de integração concreto; ou com uma decisão anterior. Se falha, devolve **Hold — insufficient project grounding** com uma lista numerada do que inspecionar. Nunca dá Adopt nem Reject com esse piso falho;
      - **piso externo:** passa com pelo menos uma fonte externa verificada cujo texto sustenta a afirmação. Se falha, devolve **Hold — external evidence unavailable**, nunca uma nota com confiança reduzida.
   5. **Documento ou conjunto de abordagens:**
      - o piso de projeto é o mesmo. Se falha, devolve **Blocked — insufficient project grounding** com a lista do que inspecionar;
      - fonte externa só é exigida para afirmações externas de que a conclusão depende. Se uma delas não tem fonte, devolve **Blocked — external evidence unavailable** com a lista da evidência que falta.

6. **Fase 3: forma a posição** (`method.md`)
   1. Forma a própria posição sob o contrato do formato do assunto, mas não a mostra. Congela essa posição, que fica fora do contexto inicial de qualquer peer independente.
   2. **Adoção externa:** dá exatamente uma nota:
      - **Adopt:** encaixe comprovado, usar;
      - **Trial:** promissor; usar primeiro numa fatia de baixo risco, com um spike delimitado como próximo passo;
      - **Hold:** uma decisão completa de esperar. As duas falhas de piso também são Hold;
      - **Reject:** não vale a pena para nós;
      - **Not-our-problem:** um CVE ou uma deprecação que não nos atinge.

      Preserva a nota, o incumbente ou o ponto de integração, as evidências verificadas, as condições materiais e o que mudaria o veredito. Afirmações da conversa não verificadas ficam separadas. Uma próxima ação recomendada só entra quando faz sentido e não dá permissão para executá-la.
   3. **Visão de um documento:** julga a direção geral, não lista achados. Traz a conclusão, os pontos fortes, os riscos e os fatos de projeto que a determinam. Aplicar revisões é trabalho do workflow dono do documento.
   4. **Posição sobre abordagens:** escolhe quando a evidência dá base. Se as opções são viáveis de verdade, diz **"Either is viable"** e explica os tradeoffs, sem placar nem contagem de vantagens. Se compará-las exige desenvolver soluções, encaminha para o `ce-bakeoff`.
   5. **Decide sobre o painel.** Se houve pedido de painel, ou se a posição pode merecer uma oferta proativa, lê `cross-model-panel.md` e termina o passo 7 antes de escrever. Senão, vai direto para o passo 8.

7. **Painel entre modelos** (`cross-model-panel.md`, só com pedido ou oferta aceita)
   1. **Identifica o assunto e o host.** Resolve atalhos da conversa. Atesta o host pelas variáveis de ambiente: `CLAUDECODE=1` dá `claude`; as `CODEX_*` dão `codex`; `GROK_AGENT` ou `GROK_SESSION_ID` dão `grok`; as `CURSOR_*` dão harness `cursor` com família `unknown`; `OPENCODE_TERMINAL` dá `opencode` com família `unknown`.
   2. **Separa quatro identidades** de cada participante: o target (`codex`, `claude`, `grok`, `cursor`, `composer`), a rota (o CLI que roda), o modelo pedido e o modelo servido. O modelo servido só é conhecido por recibo; sem recibo fica `unverified`. `independence_verified` só é `true` quando a família servida é comprovadamente diferente da do host.
   3. **Escolhe exatamente um ramo de participação:**
      - **peers nomeados:** roda exatamente os nomeados, sem limite, e nunca troca `Cursor` por Composer nem um modelo pedido por outro;
      - **`oracle` sozinho:** escolhe até dois targets acessíveis e comprovadamente diferentes, por esta ordem: preferência da conversa, config local, convenções do projeto, ordem padrão. Anuncia e roda;
      - **cross-check explícito sem nomes:** pula a checagem de custo e usa a regra de contagem. Com zero peers acessíveis, entrega a posição solo com uma linha de disponibilidade. Com um ou mais, escreve uma linha de progresso com os escolhidos;
      - **sem pedido:** depois de formar a posição, oferece o painel só quando trabalho importante vai se apoiar nela antes de um erro aparecer, ou quando ela alimenta um compromisso compartilhado, público, de segurança ou de dados. Adoção Tier 1 nunca recebe a oferta; Tier 2 e 3 podem. Uma invocação warm nunca oferece;
      - **skeptic:** se o pedido é desafiar a posição do `ce-pov`, os peers rodam em `mode: skeptic`.
   4. **Fixa o escopo.** Normaliza o que os peers podem ler: uma raiz relativa ao repositório, mais padrões de inclusão e exclusão. O padrão é a raiz do repositório, e um escopo mais estreito nunca é alargado. Os padrões são cooperativos: a skill nunca promete que segredos dentro do escopo ficam inacessíveis. Registra a identidade do repositório: o commit atual mais um digest do conteúdo sujo e não rastreado dentro do escopo.
   5. **Resolve e anuncia uma rota fixa por peer:**
      1. sonda a rota sem passar conteúdo do projeto;
      2. tenta o mapeamento preferido;
      3. se ele não serve, usa o equivalente mais próximo no mesmo target, família e tier de raciocínio. Um modelo que você pediu nunca vira outro;
      4. confere que todo destinatário está na lista permitida;
      5. anuncia em linguagem simples quem vai inspecionar e que é só leitura, sem detalhes técnicos.

      As rotas aceitas são `codex`, `claude`, `grok-cli`, `grok-cursor`, `cursor`, `composer` e `opencode`. Para Grok, usa `grok-cli` quando o CLI existe e `grok-cursor` só se você pediu ou se o CLI falta e o Cursor é permitido.

      **Pede sua aprovação** em dois casos: quando uma nova tentativa acrescentaria um destinatário ou intermediário inesperado, e quando uma instrução sua, do projeto ou da organização exige aprovação para consulta externa. Num sandbox do Codex sem rede, pede a permissão `require_escalated` na chamada de `start`. Se ela é negada, aquele peer não roda.
   6. **Monta o payload**, um arquivo com modo 600 na pasta temporária. Ele leva a pergunta enquadrada, o formato do assunto, o escopo, a identidade do repositório, o modo, os caminhos dos arquivos do assunto e o material que só existe na conversa, com rótulo. Na primeira rodada:
      - nada da posição do `ce-pov`, nem conclusões de outras vozes, nem interpretações, rankings de risco ou rótulos avaliativos do host;
      - diz que rejeitar todas as opções, ou o próprio enquadramento, é uma posição válida;
      - se o `ce-pov` escreveu as opções nesta sessão, apresenta todas de forma simétrica;
      - se o assunto já é uma posição formada (a do `ce-pov` ou a sua), ela vai inteira como assunto;
      - no modo skeptic, inclui a posição do `ce-pov`, porque criticá-la é a tarefa.

      O mesmo payload precisa caber em todas as rotas. Ele nunca é cortado por provedor.
   7. **Dispara um job por peer**, todos antes de esperar, com `scripts/peer-job-runner.py start --skill ce-pov --run-id <id> --label <target> --result-path <run-dir>/pov-<target>.json -- env ... bash scripts/cross-model-pov.sh <família-do-host> <rota> <payload> <run-dir>`. Em cada chamada procura um Python que funcione (`python3`, `python`, `py`) e limpa `CE_PEER_HARD_SECS`.
      - **O `start` do `peer-job-runner.py`:**
        - valida os identificadores e cria a pasta do job, só do dono, em `<jobs-root>/ce-pov/<run-id>/jobs/<job-id>/`, com `meta.json` e `out.log`;
        - confere que o worker existe e apaga runs irmãs com mais de 24 horas;
        - desacopla um supervisor (double fork com `setsid`; no Windows, um processo destacado com Job Object) e imprime só o id do job;
        - o supervisor checa a cada 2 s. Mata o worker se `out.log` fica 240 s sem crescer, se o tempo passa de `max(1230, CROSS_MODEL_HARD_SECS + 30)` segundos, se o log passa de 10 MB ou se o resultado passa de 5 MB;
        - grava `status` de forma atômica com `done`, `failed`, `timeout` ou `died-without-result`.
      - **O `cross-model-pov.sh`:**
        - valida as entradas: payload existente, raiz de leitura dentro do repositório, run dir já criado e fora do repositório, `jq` instalado, tokens de host e rota válidos, override de modelo da mesma família;
        - lê `references/agents/pov-peer.md` e `references/pov-schema.json`;
        - respeita a lista `CROSS_MODEL_PEERS`, quando definida;
        - pula, sem cortar, um payload acima de 200.000 bytes;
        - procura o `codex` dentro de `ChatGPT.app` ou `Codex.app` quando ele não está no `PATH`;
        - monta o prompt com a persona, uma linha de autorização, o schema, o bloco de escopo e o payload;
        - roda o CLI da rota só em leitura, com raciocínio alto:
          - `codex exec -s read-only` com `gpt-6.1-sol`;
          - `claude -p` com `claude-opus-5-5`, só com `Read`, `Glob`, `Grep`, `WebSearch` e `WebFetch`, até 15 turnos;
          - `grok` com `grok-4.7` em `xhigh`, negando edição, escrita, Bash, tarefas e MCP;
          - `cursor-agent` em modo `ask` com sandbox: `grok-4.7-xhigh`, Auto ou `composer-2.5-fast`;
          - `opencode` com edição, bash, webfetch e tarefas negados;
        - corta o peer depois de 180 s sem saída ou 600 s no total (`grok-cli` só tem o limite total) e escreve um sinal de vida a cada 60 s;
        - extrai o JSON da posição. Só a rota `claude` devolve um recibo do modelo servido;
        - se a posição vem com `final: false`, tenta mais uma vez na mesma rota, pedindo a resposta final, desde que sobrem pelo menos 60 s. Senão, descarta com `peer skip evidence: non-final position`;
        - normaliza a voz para `peer-<target>`, acrescenta rota, harness, família, modelo pedido, modelo servido e `independence_verified`, e grava `<run-dir>/pov-<target>.json` com modo 600;
        - se não sai nada utilizável, registra um trecho curto de stdout e stderr como evidência da falha;
        - sempre sai com código 0 e apaga a pasta de trabalho do peer.
   8. **Espera** com `peer-job-runner.py wait --max-secs 30 --json <job-ids>`, repetido até todos terminarem ou até o prazo total de `CROSS_MODEL_HARD_SECS` + 10 s (610 s por padrão). No prazo, roda `reap` em cada job que ainda corre (sinaliza o supervisor; se ele sumiu, mata a árvore e grava o status) e faz um último `wait --max-secs 10`.
   9. **Coleta** pelo `result <job-id>` do runner, uma leitura limitada que confere o dono do arquivo e sai com 0 (pronto), 2 (rodando), 3 (outro estado) ou 4 (ilegível). Aceita só um artefato no formato do schema, com `final: true`, `reasoning` não vazio, `movement` válido e os recibos de rota e modelo. Na primeira rodada, `movement` precisa ser `initial`.
      - Sem artefato utilizável, cita a evidência observada (cota, autenticação, falha de rota) e nunca inventa causa.
      - Só diz que sua conta está deslogada se ficou provado que o pedido chegou ao provedor.
   10. **Se os próprios scripts falham** (crash, saída não zero antes de o job subir, caminho não resolvido), tenta a mesma rota à mão, com o mesmo target, modelo, escopo e regras de independência. Continua só enquanto cada falha é nova e o prazo não acabou. Depois, cai na posição solo.
   11. **Reconcilia** (só vozes `independent` entram).
       - Há divergência material quando muda a nota, a abordagem escolhida, a ação que o leitor tomaria sobre um documento (`proceed`, `revise-first`, `reject`) ou a avaliação de se um risco é fatal. Diferença de palavras, ênfase ou confiança é concordância.
       - O limite padrão é a rodada inicial mais até duas trocas. Um limite seu prevalece: "one pass" ou "one round" significa nenhuma troca.
       - Em cada troca:
         1. revalida a identidade do repositório. Se mudou, reinicia todas as vozes ou devolve um painel incompleto;
         2. reconsidera cada posição;
         3. verifica as afirmações de projeto em disputa que mudariam a decisão: `verified`, `contradicted` ou `unverifiable`;
         4. manda a todos o mesmo delta de evidências, o assunto completo e a posição de cada voz, com até 5 evidências por voz;
         5. resolve as rotas de novo e dispara uma rodada nova. Um peer que falhou sai das rodadas seguintes.
       - Na reconciliação, cada peer responde `moved` ou `held` e explica.
   12. **Para** no primeiro caso que vale:
       - **`confident`:** o `ce-pov` tem uma posição fundamentada depois de pesar todos os sobreviventes;
       - **`no-movement`:** todos responderam `held` e o `ce-pov` ainda não está confiante;
       - **`limit-reached`:** o limite acabou depois de uma divergência e ainda não há confiança.

       Convergência é a confiança do `ce-pov`, não um voto. No limite, só recomenda mais um número definido de trocas se consegue nomear a questão aberta, a nova evidência e por que ela moveria alguém; senão, recomenda parar. Rodadas extras precisam da sua aprovação, a menos que você já tenha dado um limite maior, e cada aprovação cria um novo limite finito.
   13. **Modo skeptic:** incorpora uma vez a crítica atribuída, sem levá-la à convergência, e diz se ela mudou a posição.
   14. **Limpa** pastas de job, saídas das rodadas, payloads, logs e resultados da pasta temporária da run, em qualquer desfecho. Nunca apaga fora dela.

8. **Entrega a posição** (`method.md`, `cross-model-panel.md`)
   1. Escreve o bloco do chat pelo `ce-noslop`.
   2. Abre com a decisão. Mantém as evidências citadas, os tradeoffs, a incerteza e as condições que mudariam o julgamento. Torna os identificadores compreensíveis sem reabrir o assunto. Não reproduz dossiês nem a saída crua dos peers.
   3. **Nota do painel**, só se houve pedido de painel:
      - **Confident:** diz se as vozes concordaram. Se decidiu contra uma divergência, nomeia a divergência e por que prevaleceu;
      - **Stalemate:** a posição atual do `ce-pov`, a de cada peer com o movimento, o último estado das vozes que caíram e se a divergência é de evidência ou de julgamento. Recomenda quando há base; senão, "Either is viable" com os tradeoffs. No limite, acrescenta **Further rounds:** com uma extensão definida ou a recomendação de parar;
      - **Partial:** quem sobreviveu, quem caiu e a falha observada (cota, autenticação, timeout, posição não final);
      - **No survivor:** a posição solo com "cross-model check unavailable or incomplete".

      Se houve pedido e o painel não rodou, diz quais peers foram tentados, ou que nenhum rodou e por quê. Sem pedido, não há nota.
   4. Cita cada peer pelo target e pelo modelo pedido. Só acrescenta ressalva quando o recibo diverge do pedido, quando nenhum modelo foi pedido ou quando a independência afeta a credibilidade. Nunca atribui posição a um modelo que não rodou.

9. **Fase 4: entrega e devolve** (`followup.md`, `report.md`)
   1. **Chamada por outro workflow:** devolve o resultado e o controle, sem menu nem oferta de registro.
   2. **Chamada direta:** a posição já completa a skill e nenhuma escolha de próximo passo é exigida. Só passa para outra skill se as quatro condições valem juntas:
      - o pedido original autorizou explicitamente aquela ação seguinte;
      - o resultado resolve a decisão e não é um impasse;
      - a ação fica no escopo herdado;
      - a ação não é destrutiva e está autorizada de outro modo.

      Uma recomendação sozinha não autoriza implementar. Se a escolha importante continua sendo sua, devolve essa escolha; senão, termina com o resultado.
   3. **Continuação autorizada:** escolhe pelo resultado real, não por um menu fixo. Adoção pode ir para planejamento (`ce-plan`) ou para um trial; a visão de um documento, para revisões no workflow dono dele; uma posição sobre abordagens, de volta ao design ou à execução. Invoca a skill responsável com a decisão, as evidências, as condições e o escopo herdado. Nunca supõe que todo resultado positivo precisa de um plano.
   4. **Relatório, só se você pedir** (`report.md`):
      - expande o julgamento para os leitores e o destino, mantendo o formato do assunto: uma visão de documento não vira relatório de adoção;
      - usa o formato pedido ou o mais útil, cita as fontes e separa inferência de evidência;
      - grava um arquivo avulso na pasta temporária privada e informa o caminho. Antes de gravar num local do projeto, resolve o `<root>`;
      - se outro workflow é dono do documento em volta, devolve o conteúdo para ele incorporar;
      - só publica se você pedir, com as ferramentas do destino. Se a publicação falha, mantém o arquivo local e informa.
   5. **Registro durável, só se você pedir:** invoca o `ce-compound` com `mode:non-interactive` e a decisão estruturada num tipo de registro que já existe.

## Rota warm

`invocation.md` vale quando o `ce-pov` entra no meio de uma sessão, como segunda opinião. O método é o mesmo, com quatro diferenças:

- **Só a pergunta vem da conversa.** A conversa fornece a pergunta e as afirmações a verificar, nunca evidência. "Temos 40 pontos de chamada em X" só conta depois que um scout ou uma leitura da fonte confirma.
- **Checagem do enquadramento.** Sem pergunta explícita, ou com uma pergunta ambígua, aplica a resolução da fase 0 e devolve o contexto que falta. Se você nomeou a pergunta ("ce-pov: should we use X?"), pula a checagem.
- **Mais adversarial:**
  - faz uma passada explícita de evidência contrária sobre cada afirmação da conversa de que o veredito depende;
  - nunca sobe a nota só porque a sala já quer aquilo.
- **Comportamento de convidado:**
  - só consulta peers se o pedido warm pede explicitamente, e nunca oferece painel;
  - entrega só a posição pedida, sem reenquadrar a sessão nem assumir o brainstorm;
  - a posição é a última coisa que a skill escreve, e a sessão anfitriã segue dali;
  - não oferece registro, a menos que você peça.

Um pedido de painel warm que aponta uma posição já formada (a anterior do `ce-pov` ou a sua) a trata como assunto: os peers formam o próprio veredito sobre ela. Um novo pedido de painel depois de você contestar abre uma rodada nova antes de qualquer mudança de posição.
