# ce-prototype passo a passo

O que a skill `ce-prototype` do plugin Compound Engineering faz, da chamada até o handoff.
Lido do `SKILL.md`, das 6 referências, do script `light-webserver.js` e dos 2 assets (`annotate.js`, `annotate.css`) de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06). A skill não tem `agents/`.
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Lê a entrada** (`SKILL.md`)
   1. Aceita como argumento um prompt, o caminho de um brainstorm, o caminho de um plano ou nada. Sem argumento, usa a conversa: se ela já diz o que testar, parte dali.
   2. **Confere se há uma pessoa.** Com `mode:pipeline`, numa run headless, ou quando a skill que chamou diz que não há humano, ela para:
      - não sobe preview e não inventa como algo deveria parecer;
      - devolve que precisa de um humano e mostra como rodar de novo com alguém presente (`/ce-prototype`).

      Uma skill chamadora com humano presente, como um `lfg` interativo, conta como run normal.
   3. **Regra de invocação:** toda invocação que ela imprime usa `/ce-prototype`, `/ce-brainstorm` e `/ce-plan`. Só no Codex, ou num host que documenta o prefixo `$`, usa `$ce-prototype` etc. Imprime uma forma só, como código inline.
   4. **Regra central, que vale para a run inteira:** não falsificar a dimensão que está sendo testada. Pergunta sobre fluxo ou estado se resolve dirigindo o protótipo; pergunta sobre layout ou marca se resolve vendo o acabamento real. Quem decide é a sua percepção, nunca o julgamento dela sobre o artefato.

2. **Escopo: obtém a pergunta** (`scoping.md`, leitura obrigatória antes de perguntar qualquer coisa ou tocar no repositório)
   1. Se inferiu a pergunta de um histórico confuso, diz o que inferiu. Só pergunta quando não consegue dizer.
   2. Lê a conversa e o brainstorm ou plano que você passou. Num repositório, o produto é o desse repositório, a não ser que você diga outra coisa.
   3. **Leitura do repositório por subagente.** Se o repositório precisa ser consultado, dispara um subagente genérico pela primitiva da plataforma (`Agent` no Claude Code, `spawn_agent` no Codex), nunca um agente nomeado ou tipado. A skill não define tier nem orçamento de leituras.
      - Pede só o que a pergunta toca: a página, o componente ou o fluxo a recriar, com caminhos de arquivo, o que faz hoje e as restrições sobre ele.
      - Nem ela nem o subagente varrem a árvore, e ninguém pede resumo de arquitetura.
      - Sem primitiva de subagente, faz a mesma leitura restrita no próprio contexto.
      - Depois lê ela mesma os arquivos quando precisa do detalhe.
   4. **O que precisa ter antes de construir:** a superfície do produto, a pergunta e as restrições duras (o que tem de ficar, o que não pode mudar). Se faltar superfície ou restrição, pergunta. Nunca pede que você invente a resposta no chat.
   5. Nomeia as partes que só se decidem diante de um artefato real, ou usa a pergunta que você nomeou. Começa pela mais cara de errar. Junta partes num protótipo só quando a pergunta é como elas funcionam juntas.
   6. Se o brainstorm ou plano já registra uma decisão de visual probe resolvida para esta pergunta, não a reconstrói.

3. **Classifica a pergunta como narrow ou wide** (`scoping.md`)
   - **Narrow** (um detalhe com poucas opções parecidas: este controle ou aquele, esta posição, esta transição): 2 ou 3 variantes próximas numa superfície só, sem inventar um mecanismo muito diferente.
   - **Wide** (espaço aberto, como "deixa isso mais divertido"):
     1. diverge primeiro: nomeia de 3 a 5 caminhos com mecanismos diferentes, não ajustes de uma ideia;
     2. descreve cada um numa linha simples sobre o que você veria ou faria, sem nome inventado e sem veredito;
     3. você escolhe, ou ela põe um subconjunto comparável numa superfície;
     4. converge a partir do que foi construído.
   - **Incerto:** pergunta uma vez se é uma comparação próxima ou uma exploração aberta. Não assume nenhum dos dois.

4. **Dimensiona o build** (`scoping.md`)
   - O tamanho segue a incerteza, não a vontade de fazer pequeno. A aposta ambiciosa, a combinação que só faz sentido junta e o espaço aberto sem mecanismo pedem um build maior. Uma escolha separável cabe num menor.
   - Não começa pela pergunta fácil só porque é barata. Essa divisão é decidida antes do go-ahead, porque ele a nomeia.

5. **Pede o go-ahead quando ele é devido** (`scoping.md`)
   1. Se o pedido já é prototipar uma coisa nomeada, isso é a autorização depois do escopo.
   2. A mensagem é devida quando o pedido é só "vale prototipar?", ou quando a divisão ou uma inferência surpreenderia você.
   3. A mensagem fica em alto nível: o que vai tentar, por quê e como divide. Diz se inferiu de um histórico confuso e deixa espaço para você nomear outra pergunta.
   4. Espera "proceed" ou correção. Nada é construído sem autorização.
   5. Depois que você autoriza, só fala quando há algo novo que você pode usar, numa linha: a tela está no ar, a URL está viva, ou há um bloqueio que só você remove.

6. **Escolhe o meio e a fidelidade** (`SKILL.md`, `build.md`)
   1. **Ver ou dirigir.** Se a decisão cai sobre o resultado renderizado (como lê um layout, o que faz uma paleta, densidade), é pergunta de ver e ela carrega `craft-floor.md`. Se cai sobre o que acontece quando você navega (fluxo, modelo de estado, resposta de um controle), é pergunta de dirigir e não carrega.
   2. **Fidelidade** é outro eixo, separado do tamanho. Descartável quer dizer sem manutenção e sem entrega, não raso: sem testes, abstrações ou endurecimento além do executável, mas com o acabamento que a dimensão pede.
      - fluxo ou estado: rico o bastante para dirigir;
      - direção visual: acabado o bastante para julgar;
      - posicionamento: fica fino.

      Pode variar por caminho numa run wide. Só persiste estado quando a persistência é a pergunta.
   3. **Web por padrão**, seja qual for a linguagem do produto (um app nativo ganha uma aproximação web, não SwiftUI). Só sai da web em dois casos:
      - você nomeia uma tecnologia;
      - a dimensão não se renderiza num navegador sem falsificar. Aí constrói no meio que a dimensão exige e diz isso antes de construir.

      Se a tecnologia nomeada também não renderiza a dimensão, avisa em vez de ceder calado. Uma run fora da web segue a "Rota overlay e mídia não-web" no fim deste documento.
   4. **O artefato na web** é o que um navegador mostra e ela consegue escrever: HTML, SVG, CSS, imagens, dentro da página que o helper serve.
      - Se o host gera imagens, usa.
      - Se não gera, escreve as opções em markup quando o markup carrega a dimensão honestamente, e diz isso.
      - Se não carrega (direção fotográfica ou pictórica), relata a capacidade que falta em vez de falsificar.
      - Nunca cria um segundo mecanismo de exibição ao lado da página.
   5. **Recria, não reconstrói o app.** Recria só o que a pergunta precisa. O app inteiro só sobe se a pergunta é a sensação do produto todo.
   6. **Overlay no app real** só quando você pede, ou quando a pergunta é densidade ou chrome de uma página existente (uma página isolada esconderia isso). Esse é o único caminho que toca na árvore do produto e segue a rota do fim deste documento.

7. **Resolve o diretório da run** (`preview.md`, `build.md`)
   1. **Decide a durabilidade antes do bloco**, que lê as decisões uma vez só:
      - se você pediu para a run não ficar no repositório, usa `RUN_KEEP="no"` e vai para o temp do sistema;
      - senão, dentro de um repositório git, testa `git -C <repo root> check-ignore -q .context/compound-engineering/` (a barra final é obrigatória);
      - se o caminho não está ignorado, oferece acrescentar essa linha ao `.gitignore` da raiz e só acrescenta se você aceitar, sem mexer no resto do arquivo. A oferta vem antes da resolução, ou aceitar não ajudaria esta run. Uma run que vai para o temp de qualquer jeito não recebe a oferta.
   2. **Roda o bloco de resolução uma vez por invocação.** Ela não cria o diretório por conta própria, porque uma segunda criação separaria as telas do `decisions.md`. O bloco:
      1. escolhe `<repo>/.context/compound-engineering` quando a run é mantida, há repositório, `.context` não é symlink e o caminho está no gitignore; senão usa `/tmp/compound-engineering-<uid>` (ou `$TMPDIR/compound-engineering-<uid>` se `/tmp` não é seguro);
      2. checa a raiz e o subdiretório `ce-prototype/`: não pode ser symlink, tem de pertencer ao usuário, é criado com `umask 077` e recebe `chmod 700`;
      3. se a raiz do repositório falha qualquer checagem, cai para o temp; se o temp também falha, aborta com "no usable run root";
      4. reivindica `<base>/ce-prototype/<YYYY-MM-DD>-<slug>` com `mkdir` atômico; se o nome existe, tenta `-2`, `-3` até `-99`;
      5. imprime o caminho absoluto, que é reusado em todas as chamadas seguintes.
   3. **Cai no temp** quando você recusa a linha do `.gitignore`, quando pediu para não deixar a run no repositório, quando não há repositório git ou quando o caminho falha nas checagens de segurança. Lá a sobrevivência é só melhor esforço, e ela não promete prazo. Chamar o protótipo de descartável não é pedido para sair do repositório.
   4. **Cria o diretório da pergunta** `<run>/<NN>-<question-slug>/` com outro bloco, que reconfere a run (sem symlink, do usuário), cria com `umask 077` e `chmod 700` e imprime o caminho. O `--root` do servidor é sempre esse diretório, nunca o da run.

8. **Constrói as telas** (`build.md`, `craft-floor.md` em pergunta de ver)
   1. **Piso de qualidade** (só em pergunta de ver, e só os itens que a pergunta põe em jogo):
      - **contraste:** 4.5:1 no texto, 3:1 em texto grande, controles, foco e gráficos com significado;
      - **medida:** 65 a 75 caracteres por linha;
      - **espaçamento:** um ritmo só para a superfície;
      - **escala tipográfica:** degraus óbvios;
      - **estados:** só os que a superfície tem, com conteúdo real nos valores mais curtos e mais longos;
      - **foco de teclado** visível;
      - **movimento:** no máximo um momento autoral;
      - **texto:** controles nomeiam a ação, erros dizem o que fazer.

      Mede o resultado renderizado em vez de supor.
   2. **Especificidade.** Trata como sinal de template e refaz:
      - bloco de abertura centralizado com dois botões;
      - fileira de cards iguais com ícone num círculo;
      - gradiente atrás do título;
      - rótulos pequenos em caixa alta acima de cada seção;
      - o mesmo raio em tudo;
      - numeração sobre o que não é sequência.

      O teste: sem o nome e o texto do produto, dá para saber que produto é?
   3. **Caminhos numa pergunta de ver** diferem pelo princípio de organização, não por trocar paleta ou fonte. Ela diz o princípio de cada um antes de construir. Se dois cabem na mesma frase, são um caminho só.
   4. **Mostra as opções juntas** numa superfície quando a pergunta é qual vence, a não ser que isso distorça o julgamento: rolagem ou transição ganham uma tela em tamanho cheio, e a superfície de comparação fica estática. Quando um controle troca opções no lugar (abas), cada contêiner recebe `data-ce-variant="<nome>"` com nome único, para os pins ficarem só na opção certa.
   5. Grava em `<pergunta>/screens/001-<variant>.html`. Os assets ficam em `screens/` nos caminhos que a tela pede (como `screens/img/blot.webp`), ou viram data URIs. Depois de cada ação ou troca de variante, mostra o estado relevante.

9. **Sobe o preview** (`preview.md`)
   1. Usa só o `scripts/light-webserver.js` da própria skill (cópia idêntica ao do `ce-brainstorm`), nunca o de outra skill. Define `SKILL_DIR` como o diretório do `SKILL.md` carregado e o redefine em cada comando, junto com `PROTO_DIR`, porque variáveis de shell não persistem. Antes de cada chamada, reconfere se `PROTO_DIR` não é symlink e pertence ao usuário, porque o servidor confia no `--root` sem checar.
   2. **Comandos:**
      - inicia em segundo plano com `node "$SKILL_DIR/scripts/light-webserver.js" start --root "$PROTO_DIR" --annotate` (runs web isoladas sempre usam `--annotate`);
      - acrescenta `--foreground` se o host mata processos destacados ou a URL morre depois da chamada;
      - `status` e `stop` usam o mesmo `--root`.
   3. **O que o script faz:**
      - **`start`**: cria `screens/` e `state/`. Se já há um servidor vivo para essa raiz, no mesmo modo e com sessão aberta, devolve os dados dele; se está no outro modo, derruba. Senão lança um filho destacado (`serve`), com log em `state/server.log`, espera `state/display-info.json` e imprime JSON com a URL `http://localhost:<porta>`.
      - **`serve`**: escuta em `127.0.0.1` numa porta livre e grava `state/server.pid` e `state/display-info.json`. Serve em `/` a tela `.html` mais nova de `screens/` e qualquer outro arquivo de `screens/` no mesmo caminho, recusando o que resolve para fora (inclusive por symlink). Expõe `/version`.
        - Com `--annotate`, gera um token de sessão, põe um cookie na primeira visita e injeta o overlay (`/__ce-annotate/annotate.js` e `annotate.css`) em todo documento navegado, sem mexer nas telas em disco.
        - As rotas `/wait`, `/annotation`, `/session/flush`, `/session/end` e `/events` exigem o token.
        - Avisa a página por SSE quando algo em `screens/` muda, e a página recarrega.
        - Sem `--annotate`, a página consulta `/version` a cada segundo e só recarrega quando a tela mais nova muda.
      - **Fim da sessão**: a sessão termina por `user-ended` (End session), `tab-closed` (sem aba conectada por 30 s), `idle` (30 min sem atividade; aba aberta não conta, `wait` parado conta), `owner-exited` (o processo do harness morreu) ou `stopped` (`stop`, SIGTERM). Grava `session_ended` em `display-info.json`.
      - **`wait`**: bloqueia na rota `/wait` e devolve o próximo lote de anotações. Saída 0 com um array JSON, 1 com `session-ended` e o motivo, 2 em erro.
      - **`stop`**: manda SIGTERM, depois SIGKILL, e apaga o pidfile. **`status`**: informa se está rodando.
   4. **Se `SKILL_DIR` não resolve, o helper não existe ou o host não mostra uma URL local direito**, ela para e relata. Não resolve a pergunta conversando.
   5. **Confere a tela antes de passar a URL:**
      - tira screenshot quando a plataforma permite; senão mede o layout no DOM;
      - confere cada variante em repouso;
      - só dirige uma interação quando o efeito dela não aparece em repouso;
      - lê depois que tudo assentou.

      Um 200 em todos os assets não basta. Se não tem como ver a tela, diz isso ao entregar a URL.
   6. **Entrega uma URL, para um navegador só:**
      - se o navegador está nesta máquina, usa `http://localhost:<porta>`;
      - se não está, sobe com `--host 0.0.0.0`, troca só o host da URL e avisa que a porta fica exposta à rede, o que só se faz numa rede em que você confia.

      Nunca mostra o token.

10. **Roda o loop de anotação** (`annotation-loop.md`, só em preview web isolado)
    1. Antes do primeiro `wait`, diz numa linha:
       - que a URL está viva;
       - que **Annotate** prende uma nota;
       - que **Ctrl+A** congela o hover para anotá-lo;
       - que **Esc** ou Ctrl+A de novo desliga a anotação;
       - que **Send to agent** manda as notas como um lote e mantém a sessão;
       - que **End session** devolve a conversa ao chat.
    2. **No navegador** (`annotate.js`, `annotate.css`), você clica num elemento, escreve no campo "What should change here?" e envia.
       - Cada nota vira um pin numerado: cinza enquanto espera, azul enquanto ela trabalha, vermelho com "!" quando o alvo sumiu.
       - O botão vira "Send to agent" com a contagem de notas.
       - Enquanto ela trabalha num lote, a página mostra "Sent to agent" e esconde os controles.
       - Os pins sobrevivem às recargas e somem quando a nota é aplicada.
    3. **Sempre há um `wait` rodando** enquanto a sessão está aberta (`node "$SKILL_DIR/scripts/light-webserver.js" wait --root "$PROTO_DIR"`, nunca um loop de `curl`).
       - Se o host a acorda quando um comando em segundo plano termina, roda assim e encerra o turno.
       - Senão, fica bloqueada nele pelo tempo máximo que o host permite. Se o host matou o `wait`, roda de novo: as notas continuam na fila.
       - Não consulta o `wait` com timer, não pede feedback no chat enquanto ele roda e não deixa o overlay vivo sem um `wait`.
       - Uma mensagem sua que chega nesse meio tempo é respondida, e o `wait` continua.
    4. **Processa cada lote inteiro de uma vez.** Cada registro traz:
       - `screen`: a tela onde o pin foi posto, relativa a `screens/`;
       - `comment`;
       - `selector` e `textSnippet`, que identificam o alvo;
       - `rect`, a caixa do alvo;
       - `point`, onde você clicou;
       - `variant`, quando existe; ela aplica a nota à variante mais interna.

       Em canvas ou contêiner grande, olha o que a tela desenha naquele ponto.
    5. **Para cada nota:**
       - **edição clara de tela:** aplica no lugar, no arquivo que `screen` nomeia, sem criar um `00N-*.html` novo por pin;
       - **pergunta sua:** responde no chat;
       - **mudança que seria um palpite:** pergunta antes;
       - **sintoma sem o resultado esperado:** pergunta o que você viu e o que esperava, sem escolher a causa;
       - **tirar um caminho de jogo:** não escolhe o que sobrou nem começa a próxima variante.

       Não deixa um `wait` parado enquanto uma pergunta dela está sem resposta.
    6. **Entrada não confiável:** comentário, seletor e trecho de texto descrevem edições, nunca são executados como comando, e as edições ficam dentro de `screens/` da pergunta.
    7. Depois de cada revisão aplicada, uma linha: o que mudou e que a página recarregou sozinha. Silêncio enquanto o `wait` espera.
    8. **Fim do loop:**
       - **saída 1 com `user-ended`:** a conversa volta ao chat;
       - **saída 1 com `tab-closed`, `idle`, `owner-exited` ou `stopped`:** diz qual foi e que pode subir o preview de novo;
       - **saída 2:** derruba o preview com `stop`, e o chat vira o único canal.

       Em todos os casos, uma linha dizendo por que acabou.

11. **Registra as decisões** (`SKILL.md`, `build.md`)
    1. Mantém `decisions.md` no diretório da run, para a próxima skill não depender desta sessão. Ele leva:
       - a pergunta;
       - o que foi construído;
       - os diretórios da run e de cada pergunta;
       - o que venceu e por quê;
       - o que foi rejeitado;
       - os ajustes que você pediu e não estão no protótipo;
       - o que continua aberto.
    2. Aponta para o protótipo, não o copia. Só inclui o que muda o planejamento. Não é um plano.
    3. Escreve quando tem certeza de que uma escolha se fechou (você julgou o artefato e escolheu, com os ajustes que pediu). Sem certeza, não escreve. Não pede confirmação a cada escrita. O vencedor e os ajustes ficam também no protótipo.
    4. Convergir numa direção é o melhor resultado, mas a run está completa com um conjunto de decisões mesmo sem isso.

12. **Decide o que ainda vale construir** (`SKILL.md`, `scoping.md`)
    1. Antes da próxima pergunta relacionada, lê `decisions.md` e refaz a lista. Uma decisão pode responder outra pergunta, tornar uma inútil ou revelar uma nova. Se a lista mudou, diz o que mudou no próximo go-ahead (passo 5).
    2. Uma pergunta nova ganha seu próprio diretório (`02-<question-slug>/`) dentro da mesma run, pelo bloco de pergunta, sem resolver a run de novo. Volta ao passo 6.
    3. Se o que você decidiu mudou o que você quer construir, em vez de responder à pergunta feita, ela para e devolve o que aprendeu.
    4. Não pula para brainstorm ou plano enquanto uma pergunta relacionada ainda precisa de artefato, não começa uma campanha sem relação e para de prototipar quando você aplica.

13. **Aplica as decisões** (`SKILL.md`, `write-back.md`)
    1. **Com um brainstorm ou plano diretamente relacionado** (passado na chamada, passado pela skill chamadora ou nomeado nesta sessão como o arquivo deste protótipo), segue `write-back.md`, em Markdown ou HTML, usando `decisions.md` quando existe. Nunca escolhe um plano só porque há um no repositório.
       1. **Falha fechada.** Nestes casos não escreve nada, faz um resumo no chat e recomenda `/ce-brainstorm` ou `/ce-plan`:
          - não há caminho relacionado, ou mais de um arquivo poderia ser o alvo; nesse caso também não procura plano no repositório, não cria plano nem nota, e não escreve em `<root>/plans/`;
          - o arquivo não tem seção Product Contract; não inventa a seção.

          Duas situações pedem você antes de escrever:
          - um aviso de substituição aponta outro arquivo canônico: nomeia esse arquivo e espera você confirmar o alvo;
          - existem irmãos com o mesmo nome em formatos diferentes, sem canônico claro: pergunta qual usar.
       2. **Edita só o Product Contract** (em HTML, as seções `product-contract` e `product-requirements`):
          1. aloca o próximo R-ID livre e, quando a decisão depende de estado, o próximo AE-ID;
          2. acrescenta ou atualiza uma Key Decision com `session-settled:` (`user-directed` ou `user-approved`) e os links `Governs R…` exatos; a regra completa fica no R;
          3. resolve no lugar o texto substituído, sem camada de resoluções.

          Em HTML, cada item com ID leva a âncora (`id="r7"`) e o ID visível (`R7.`), o `session-settled:` é texto visível no card, e não entra sintaxe Markdown.
       3. **Remove o HOW.** Se o arquivo tinha planejamento de implementação, apaga inteiras Planning Contract, Implementation Units, Verification Contract e Definition of Done, sem deixar cabeçalhos vazios; em HTML tira também os links da navegação. Registra no documento que o planejamento precisa ser regenerado. Não edita Key Technical Decisions nem outra seção de HOW como conteúdo.
       4. Não copia `decisions.md` para o repositório e não cola o protótipo no plano. Edita só o arquivo recebido.
    2. **Sem arquivo relacionado, ou com relação incerta**, não cria plano nem nota. Faz um resumo a partir de `decisions.md` com as decisões e o caminho do protótipo. Numa run overlay diz que não sobrou protótipo. Esse resumo é um resultado completo.

14. **Handoff** (`SKILL.md`)
    1. **Chamada por outra skill:** devolve as escolhas de `decisions.md` e deixa a chamadora seguir.
    2. **Chamada por você:** recomenda a próxima skill, passando esta sessão como semente, pela regra de invocação:
       - depois de um write-back, `/ce-plan`, para regenerar o planejamento dos requisitos alterados;
       - numa run sem arquivo, `/ce-brainstorm` se ainda há perguntas de produto, ou `/ce-plan` se a sessão basta para planejar.
    3. Nunca apaga um protótipo mantido. O diretório é seu para limpar.

## Rota overlay e mídia não-web

**Overlay no app real** (`SKILL.md`, `build.md`), quando você pede ou a pergunta é densidade ou chrome de uma página existente:

- edita os arquivos do produto como um overlay descartável;
- não usa o loop de anotação: o feedback vem pelo chat;
- nunca faz commit do código do protótipo no branch do produto;
- quando a tentativa acaba, restaura só os arquivos que ela mudou, nunca trabalho que não é dela;
- se não consegue desfazer limpo, lista os arquivos que ficaram modificados em vez de entregar a árvore suja;
- não deixa artefato, e o resumo final diz isso.

**Mídia não-web** (`build.md`), quando você nomeia uma tecnologia ou a dimensão não cabe num navegador:

- ela anuncia o meio antes de construir;
- mostra o resultado como esse meio mostra, sem o helper web;
- o feedback vem pelo chat.

Nas duas rotas, os passos 11 a 14 valem do mesmo jeito.
