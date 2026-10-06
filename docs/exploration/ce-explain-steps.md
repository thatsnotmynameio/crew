# ce-explain passo a passo

O que a skill `ce-explain` do plugin Compound Engineering faz, da chamada até a entrega.
Lido do `SKILL.md`, das 6 referências e dos 2 prompts de subagente em `references/agents/` de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06). A skill não tem scripts nem assets.
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Lê a entrada** (`SKILL.md`)
   1. Usa o argumento como a pergunta, o conceito, a mudança ou a janela de trabalho, junto com o uso ou o leitor pretendido.
   2. Define o que conta como pronto: a explicação com a evidência e as perguntas que ficaram sem resposta, ou o bloqueio específico. Se você pediu um artefato, entrega o artefato e o caminho dele. Publicar é uma ação à parte e não é condição para terminar.
   3. Aplica as regras de interação, que valem para a run inteira:
      - ajusta profundidade e apresentação ao leitor e ao uso, nunca só a partir de quem chamou;
      - resolve primeiro o que dá para descobrir sozinha;
      - só pergunta quando a falta de informação muda a resposta e nem o pedido nem a evidência resolvem;
      - sem interação disponível, devolve a pergunta pendente e a consequência dela, sem esperar e sem inventar;
      - pode explicar um comportamento verificado e dizer que a razão histórica é desconhecida.
   4. **Se foi chamada por outro workflow:** entrega o resultado pedido e deixa a continuação com o dono do workflow. Não acrescenta menu de destino nem oferta de próximo passo.
   5. Lê `orchestration.md` antes de levantar evidência, antes da primeira pergunta bloqueante e antes de despachar qualquer subagente.

2. **Fase 1: classifica a pergunta e o uso** (`intake.md`)
   1. Classifica o pedido em exatamente uma forma: **concept**, **diff**, **idea** ou **recap**. Faz isso raciocinando sobre o texto, sem depender da substituição de argumentos do harness.
   2. **Lê os tokens de controle:**
      - `diff:<ref-ou-range>` força o modo diff (por exemplo `diff:abc1234`, `diff:main..HEAD`, `diff:PR#42`);
      - `since:<janela-ou-ref>` força o modo recap (por exemplo `since:monday`, `since:7d`, `since:v2.1.0`);
      - `output:<md|html>` troca o formato do artefato, que por padrão é `html`;
      - `audience:<quem>` renderiza para outro leitor em vez de você.
   3. **Testa se o token é mesmo um token.** Ele precisa abrir o pedido ou estar sozinho, não ter espaço depois dos dois-pontos e, o teste decisivo, o pedido precisa continuar fazendo sentido sem ele. Se tirar o token deixa a frase quebrada, é prosa e fica no texto.
      - Um token em posição de token ganha da inferência. Dois-pontos no meio da prosa, não.
      - `diff:` e `since:` juntos são conflito: resolve o assunto pela regra de interação.
      - Um `palavra:palavra` desconhecido, como `feat:`, passa como texto.
      - Um token sem valor é prosa.
      - `output:` com valor desconhecido é descartado com o aviso `Ignored unknown output: value '<value>' — using html`.
   4. **Sem token, infere pela forma:**
      - **Diff:** o pedido nomeia uma mudança que dá para resolver, como um sha, uma branch, um PR, "o último commit", "o que você acabou de fazer".
      - **Recap:** o pedido pergunta o que aconteceu num período ("o que eu fiz esta semana", "me atualiza", "prepara meu standup"), ou só nomeia uma janela ("desde segunda passada"). Uma janela sozinha é recap, nunca um conceito.
      - **Idea:** o pedido apresenta uma proposta sua para entender ("explica minha ideia de X"). A ideia é tomada como dada.
      - **Concept:** todo o resto, um tópico, um padrão, um subsistema ou um assunto externo.
   5. **Desempata concept e diff:** se o pedido é um tópico do repositório e também nomeia uma mudança recente que dá para resolver, vence o diff, com o conceito como contexto.
   6. **Resolve a janela do recap:** token ou prosa viram o mesmo intervalo concreto de datas, que vai para o `Subject`. Só usa os últimos 7 dias quando você não nomeou janela nenhuma. Se a janela nomeada não se resolve com segurança, diz qual usou.
   7. **Checa a pegada no repositório (concept):** um conceito só se apoia no repositório quando realmente toca nele. Um assunto externo (recurso de linguagem, tópico de entrevista, paper) não recebe evidência do repositório.
   8. **Define o leitor:** por padrão é você. Quem vai falar a partir da explicação continua sendo o leitor. Se você pede conteúdo para outras pessoas, elas são os leitores. A evidência não muda, e o trabalho de outros não é atribuído a você.
   9. **Escolhe a entrega:**
      - uma resposta, ou texto para outro documento, quando isso basta;
      - um artefato quando você pede para aprender a fundo, guardar a explicação ou ter um documento independente. É HTML por padrão e markdown se você pedir;
      - `diff:` e `since:` escolhem o assunto, não obrigam a criar documento. `output:` escolhe o formato;
      - um pedido de versão curta ou de trecho para compartilhar é atendido sem documento completo.
   10. Escolhe a entrega antes de criar a pasta da run e antes de ler as regras de renderização.
   11. **Se não há assunto recuperável:** pergunta, pela regra de interação, em vez de inventar um tópico ou um artefato padrão.

3. **Resolve a raiz dos artefatos** (`SKILL.md`, só quando vai compor um caminho no repositório)
   1. Só roda quando a explicação vai ser arquivada em `<root>/explainers/` ou quando lê aprendizados em `<root>/solutions/`. Uma run só em pasta temporária, ou sobre um conceito externo, não resolve `<root>`.
   2. Lê `docs_root` só de `<repo-root>/.compound-engineering/config.yaml`, com `<repo-root>` vindo de `git rev-parse --show-toplevel`. Nunca lê de `config.local.yaml`. Sem valor, `<root>` é `docs`.
   3. Valida o valor: um diretório relativo ao repositório cujo caminho real, com symlinks resolvidos, fica dentro do repositório, não é a raiz e não está sob `.git/`. Se falha, para com um erro que nomeia `docs_root` e o valor, sem cair em `docs`.
   4. Usa `<root>` como único lugar: cria se não existe e nunca lê `docs` também.
   5. Passa aos subagentes o caminho resolvido, não o config.

4. **Fase 2: levanta a evidência** (`orchestration.md`)
   1. **Como pergunta:** usa a ferramenta de pergunta que já está na lista de ferramentas, sem chamar uma só para descobrir se ela existe. Sem ferramenta, pergunta no chat se há uma pessoa participando. Senão, devolve a informação que falta ao workflow que chamou.
   2. **Tiers de modelo:**
      - **Extraction tier:** o scout de recap e cada scout de trace. É busca e citação, no modelo mais barato capaz quando o harness permite escolher; senão, herda o modelo.
      - **Ceiling tier:** a composição da explicação, incluindo o `Check yourself`. Roda na conversa principal, no modelo da sessão, sem despacho.
   3. **Degradação:**
      - se o subagente não aceita modelo por agente, despacha no modelo herdado e mantém os orçamentos de leitura;
      - se não há subagente nenhum, faz o trabalho do scout na própria conversa, com os mesmos orçamentos;
      - erro de concorrência ou limite de agentes ativos é contrapressão: espera uma vaga e tenta de novo;
      - uma falha que continua depois de corrigir a chamada vira trabalho na própria conversa, avisado em uma linha.
   4. **Reaproveita evidência:** usa a que já existe quando é suficiente e atual. Confere de novo as afirmações sem apoio, disputadas ou afetadas por mudanças no código.
   5. **Cria a pasta da run** (`SKILL.md`), só quando um artefato ou um dossier precisa dela, e sempre antes de despachar scouts de trace:
      - a raiz é `/tmp/compound-engineering-<uid>`, criada com `umask 077`;
      - recusa a raiz se ela for symlink ou se pertencer a outro usuário; nesse caso tenta `${TMPDIR:-/tmp}/compound-engineering-<uid>` e, se ainda falhar, sai com erro;
      - aplica `chmod 700` e cria `$RUN_DIR` em `<raiz>/ce-explain/<YYYYMMDD>-<6 hex aleatórios>`, também com `700`.
   6. **Escolhe a busca pela forma da entrada:**
      1. **Entradas que tocam o repositório** (concept com pegada, diff, recap): parte das instruções do projeto que já estão no contexto e vai direto ao diff, às chamadas, ao código atual ou aos commits. Lê `CONCEPTS.md` quando o vocabulário canônico importa. Se o tópico não se delimita, faz uma única sondagem da raiz ou do workspace.
      2. **Diff:** resolve a mudança (a ref de `diff:`, ou a última mudança relevante quando o pedido aponta para ela sem nomear) e junta o diff, os arquivos tocados e o plano ou documento de solução que motivou a mudança.
         - **Range vazio ou assunto ausente:** avisa antes de explicar outra coisa. Só usa um substituto se o pedido permite ou se você concorda, e nomeia a substituição no resultado e no `Subject`. Senão, devolve o escopo pendente a quem chamou.
      3. **Recap:** não olha, não conta e não caracteriza a janela na conversa principal. Despacha direto um subagente genérico no extraction tier com `agents/work-recap-scout.md`, a janela resolvida, a raiz do repositório e `$RUN_DIR`. O scout:
         - percorre as fontes da mais barata para a mais cara: `git log` da janela (assuntos, shas, datas, autores) com `--stat` nos commits relevantes, agrupando commits relacionados; PRs abertos e mesclados só se houver interface (`gh` ou conector), senão anota "PR evidence unavailable"; e os planos, brainstorms e documentos de solução criados ou alterados na janela (`<root>/plans/`, `docs/brainstorms/`, `<root>/solutions/`), citando a decisão ou o problema;
         - escreve `$RUN_DIR/recap-evidence.md`, com no máximo 120 linhas em ordem de data: o que mudou com sha ou PR e data, o porquê citado com a fonte, e as áreas tocadas. Junta commits mecânicos numa linha de "housekeeping";
         - devolve só um resumo de 3 a 5 linhas e o caminho absoluto do arquivo;
         - com a janela vazia, não escreve nada e diz isso. A skill então informa que não houve atividade e termina sem artefato.

         Sem subagente, faz esse trabalho na própria conversa, ainda grava `recap-evidence.md` e só forma uma visão da janela quando termina.
      4. **Concept externo:** pula o repositório. Pesquisa com as ferramentas web que houver. Sem nenhuma, explica pelo conhecimento do modelo e marca o conteúdo como `Unverified — from model knowledge, not checked against current sources`.
      5. **Idea:** explica implicações, mecânica e trade-offs da ideia como você a trouxe. Nunca a delimita, que é papel do `ce-brainstorm`, nem gera e ordena alternativas, que é papel do `ce-ideate`.
   7. **Pergunta de "como":** segue o gatilho pelas mudanças de estado, pelas fronteiras de responsabilidade e pelo efeito. Lê o código e os testes; nome de arquivo ou afirmação da conversa não prova comportamento. Mantém as condições e os caminhos de falha que importam.
      1. Se uma passada nomeia as fronteiras sem rodeio, basta.
      2. Senão, divide a pergunta numa fatia por fronteira e despacha, todos juntos, um subagente genérico por fatia no extraction tier, com `agents/behavior-trace-scout.md`, a pergunta, a fatia e um caminho de dossier próprio em `$RUN_DIR`. O mínimo são 2 fatias. Mais de 4 significa que a pergunta ainda está aberta demais: estreita e refaz o trace. Cada scout:
         - lê código e testes da sua fatia, sem sair dela e sem alterar o repositório;
         - registra a entrada, o fluxo com arquivo e símbolo, as fronteiras, os caminhos de falha e as lacunas que não conseguiu seguir;
         - escreve o dossier com no máximo 120 linhas e ponteiros `file:line`;
         - devolve só um resumo de 3 a 5 linhas e o caminho absoluto do dossier, sem recomendar nada.
      3. Lê cada dossier pelo caminho e resolve sobreposição ou contradição lendo o código. O resumo não substitui o trace.
   8. **Pergunta de "por quê":** procura o registro da decisão em documentos, comentários, histórico git, discussões de PR e issues ligadas. Vai a outras fontes quando o registro local não basta, dentro das restrições do pedido. Ter acesso ao chat do time não autoriza buscar nele quando o workflow que chamou exige opt-in.
   9. **Regras da evidência:**
      - código mostra comportamento, não necessariamente intenção;
      - separa razão documentada, inferência apoiada e desconhecido, e aponta contradições;
      - busca sem resultado não prova que não houve razão;
      - confirma se uma restrição histórica ainda vale antes de apresentá-la como requisito atual;
      - se a explicação vai informar uma mudança, deixa as restrições e os riscos utilizáveis pelo próximo passo, sem escolher abordagem.

5. **Fase 3: compõe a explicação** (`SKILL.md`, `explainer-html.md` ou `explainer-markdown.md`, `check-in.md`)
   1. Responde à pergunta com a evidência, mantendo restrições e incertezas.
   2. **Confere cada afirmação antes de entregar:**
      - uma chamada de função não garante nada sobre uma implementação que não foi lida;
      - remove o que não tem apoio ou marca a incerteza no lugar, inclusive em diagramas e nas respostas dos exercícios;
      - mantém a autoria certa quando o trabalho é de várias pessoas;
      - se escolheu só parte da evidência, diz isso, e nunca apresenta um relato parcial como completo.
   3. Usa prosa, código, tabelas ou visuais quando ajudam. Não há estrutura obrigatória.
   4. **Se outro workflow vai usar a resposta:** devolve o conteúdo direto, com a evidência, as restrições que valem e as perguntas abertas. Cada trecho leva as ressalvas necessárias. Não cria artefato a menos que o uso exija.
   5. **Se o resultado é um artefato:**
      1. **HTML** (`explainer-html.md`):
         - um único arquivo HTML5 autocontido, com CSS em `<style>`, SVG inline, imagens em data URI, nenhum pedido externo e nenhuma webfont (só fontes do sistema);
         - metadados como texto visível: o `<h1>` é o título, e um `<dl>` no cabeçalho traz `Date`, `Input shape` (`concept`, `diff`, `idea` ou `recap`) e `Subject`. Acrescenta a linha `Unverified — from model knowledge, not checked against current sources` quando a evidência veio do modelo, e `Rendered for` quando o leitor não é você. Nada de JSON, `data-*` ou `<meta>` duplicando isso, e nenhum campo novo;
         - só leitura: sem formulários, handlers, scripts ou quizzes interativos;
         - classes e IDs em ASCII;
         - rodapé visível `Composed <data> by ce-explain`;
         - prosa com `max-width: 70ch`, diagramas em SVG inline com rótulos legíveis, código com destaque de sintaxe por classes do `<style>`;
         - código real da evidência para o comportamento do projeto; exemplos inventados só em tópicos externos e marcados como exemplos;
         - **auditoria:** confere que o arquivo abre sozinho, não pede nada de fora, tem os metadados visíveis e que todo visual tem equivalente em prosa.
      2. **Markdown** (`explainer-markdown.md`, quando a entrada resolveu `output:md`):
         - frontmatter YAML com `title`, `date`, `input_shape`, `subject`, `unverified: true` quando for o caso e `rendered_for` só com outro leitor;
         - markdown puro, sem HTML, sem `<details>` e sem estilos inline;
         - diagramas em blocos `mermaid`, nunca em ASCII; tabelas com pipes; todo bloco de código com a linguagem; caminhos sempre relativos ao repositório.
      3. **Check-in** (`check-in.md`, só para artefatos de ensino):
         - decide se inclui a seção `Check yourself`. O pedido manda: se você pediu quiz ou exercícios, entra; se disse que não quer, sai. Sem pedido, entra quando o objetivo é reter (conceito difícil, diff delicado, recap denso com decisões) e sai quando é só entender (recap de rotina, diff mecânico, tópico para passar o olho). Não anuncia a justificativa;
         - o leitor não muda a decisão;
         - a seção fica por último, antes do rodapé: perguntas primeiro e, sob o rótulo `Answers`, as respostas. Cada resposta diz o que uma resposta certa contém e qual lacuna uma resposta errada plausível revela;
         - é texto estático. A run nunca pergunta nada a você sobre o check-in nem espera resposta.
      4. Grava `$RUN_DIR/explainer.html` ou `$RUN_DIR/explainer.md`.
      5. Entrega um resumo no chat e o caminho do arquivo.

6. **Fase 4: entrega** (`SKILL.md`, `destinations.md` só se você pediu um destino)
   1. A resposta entregue, ou o artefato local, já completa a explicação. Não pede escolha de destino e não inventa próximo passo.
   2. Se o workflow que chamou é dono do documento em volta, devolve o conteúdo a ele, sem colocar nem publicar nada.
   3. **Se você pediu um destino**, lê `destinations.md`, resolve só a informação que falta, adapta o conteúdo ao leitor antes de pedir consentimento e confere o arquivo, a URL ou a referência resultante. Se a entrega falha, guarda o artefato local, diz o que não completou e não troca de publicador sem autorização.
      - **Claude Artifact** (só HTML, no Claude Code com a ferramenta `Artifact`): passa `$RUN_DIR/explainer.html` sem pré-processar, segue o contrato da ferramenta e confirma a URL.
      - **ht-ml.app** (o publicador HTML preferido quando o Claude Artifact não foi escolhido):
        1. Avisa que **a página é pública e pode ser indexada, rastreada, copiada ou arquivada** e pede sua confirmação explícita para aquele artefato. O pedido inicial não conta como confirmação. Se o artefato muda muito, pede de novo. Conteúdo sensível fica local.
        2. Sem confirmação, não publica e informa o caminho local de `explainer.html`. Nunca publica sem uma pessoa presente.
        3. Com confirmação, usa a capacidade de publicação de HTML que houver na sessão (uma skill, invocada com o arquivo e a sua confirmação, ou uma ferramenta, conector ou navegador). Sem nenhuma, segue `https://ht-ml.app/llms.txt` e publica o HTML completo, sem redesenhar.
        4. Mostra a URL. Trata qualquer credencial de atualização como segredo, sem imprimi-la nem colocá-la na página. Se falha, tenta mais uma vez depois de uma pausa e então volta ao caminho local.
      - **Arquivo local:** se você não deu o caminho, pergunta. Copia `explainer.html` ou `explainer.md` da pasta da run para lá, criando as pastas que faltam, informa o caminho absoluto e abre o arquivo se você pediu e o host permite.
      - **Proof** (só markdown): invoca a skill `ce-proof` se estiver instalada, com o caminho, o título `Explainer: <subject>` e a identidade `ai:compound-engineering` / `Compound Engineering`, e mostra a URL. Sem a skill, faz POST na API web do Proof se ela responde. Se falha, tenta mais uma vez e volta ao caminho local. A cópia da pasta da run continua sendo a oficial.
      - **Thinkroom** (só se houver uma skill, uma ferramenta MCP ou uma CLI do Thinkroom): cria o documento pela interface que existir e mostra a referência. Se falha, volta ao caminho local. Sem capacidade detectada, a opção não existe.

## Fora do escopo

A skill não invoca estas skills. Ela só aponta para elas quando o pedido sai do que ela faz:

- julgar se uma abordagem deve ser adotada ou mudada é do `ce-pov`, e explicar uma escolha antiga não é endossá-la;
- guardar aprendizado no repositório é do `ce-compound`, e explicar não autoriza mexer na memória do projeto;
- gerar alternativas e delimitar implementação é do `ce-ideate`, do `ce-brainstorm` e do `ce-plan`;
- diagnosticar ou corrigir uma falha é do `ce-debug`. Explicar o comportamento atual continua sendo do `ce-explain`.
