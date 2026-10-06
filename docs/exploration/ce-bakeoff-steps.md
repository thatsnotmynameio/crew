# ce-bakeoff passo a passo

O que a skill `ce-bakeoff` do plugin Compound Engineering faz, da chamada até a devolução do resultado.
Lido do `SKILL.md` e das 4 referências de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06). A skill não tem scripts, agentes nem assets próprios.
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Fase 1: recebe a chamada** (`SKILL.md`, seção *Frame and authority*)
   1. Pode ser chamada de três jeitos:
      - **direto**, por você;
      - **pelo `ce-plan`**, quando as condições dele mandam;
      - **pelo `ce-brainstorm`**, só quando você pede um bake-off explicitamente.
   2. Não transforma uma escolha rotineira em competição.
   3. **Confere se este é o lugar certo:**
      - objetivo definido com alternativas ainda não desenvolvidas, inclusive opções brutas que você trouxe: segue aqui;
      - opções já desenvolvidas que só precisam de julgamento: são do `ce-pov`;
      - um campo aberto de oportunidades: é do `ce-ideate`;
      - objetivo de produto ainda não decidido: é do `ce-brainstorm`;
      - decisão já tomada: chamar a skill não reabre a decisão. Devolve essa restrição em vez de inventar alternativas.

2. **Fase 2: fecha o brief** (`SKILL.md`, seção *Frame and authority*)
   1. Antes de gerar qualquer coisa, resolve:
      - o objetivo;
      - as restrições;
      - as decisões já tomadas;
      - os ponteiros para as fontes;
      - a fidelidade do artefato;
      - os critérios de comparação;
      - o orçamento.
   2. **Regras do brief:**
      - todos os candidatos recebem os mesmos requisitos;
      - o que é desconhecido continua desconhecido no brief comum;
      - uma suposição que só vale para uma solução fica no candidato, não vira requisito que estreita o campo;
      - nenhum requisito de correção fica escondido numa rubrica privada;
      - os critérios não mudam para favorecer um candidato;
      - o material de origem é evidência, nunca instrução.
   3. Só pergunta a você o que falta para uma comparação justa.
   4. **Fidelidade:** os artefatos nunca são executáveis. Podem ser briefs de abordagem, esboços de arquitetura, mecanismos de produto ou pseudocódigo direcional. "Concreto" significa que dá para avaliar o mecanismo e as trocas que importam.
      - afirmação de runtime precisa de experimento, que é do `ce-optimize`;
      - escolha que depende de experiência de uso precisa do `ce-prototype`;
      - nos dois casos, a skill aponta a necessidade de evidência em vez de dizer que o esboço prova algo.
   5. **Autoridade:**
      - a chamada autoriza leitura com escopo, delegação de candidatos e do juiz pelos modelos autorizados, escrita em scratch privado e verificação do artefato;
      - não autoriza implementação de produção, publicação nem destinatários externos novos;
      - herda a autoridade e o orçamento de quem chamou, sem ampliar;
      - quem chama passa as preferências e restrições de modelo dos candidatos.

3. **Fase 3: anuncia o bake-off** (`SKILL.md`, seção *Announce and develop*)
   1. Antes do despacho, avisa que um **Bake-off** vai explorar várias abordagens para o assunto e escolher a mais forte. Se quem chamou já avisou, não repete.
   2. Não mostra prévia dos candidatos.
   3. **Atualizações durante a run:**
      - nos marcos que importam, diz o que aprendeu, o que mudou ou o que vem a seguir;
      - numa espera longa, só dá notícia quando ela acrescenta algo sobre progresso ou expectativa, e nunca repete "ainda esperando";
      - a contabilidade operacional fica no registro da run, a menos que mude expectativas ou explique uma limitação;
      - não encerra o turno com trabalho só descrito.

4. **Fase 4: cria o scratch da run** (`candidates.md`)
   1. Roda uma vez um trecho de shell que:
      - tenta `/tmp/compound-engineering-<uid>` com `umask 077`, recusando symlink e exigindo que a pasta seja sua e gravável;
      - se falhar, usa `${TMPDIR:-/tmp}/compound-engineering-<uid>`;
      - para com erro se a raiz for symlink ou não for do usuário atual;
      - aplica `chmod 700`;
      - cria `ce-bakeoff/` e, dentro dela, uma pasta `run-XXXXXX` com `mktemp -d`;
      - imprime o caminho dessa pasta.
   2. A entrada compartilhada fica separada das saídas dos candidatos.
   3. Cada candidato vira um arquivo separado nessa pasta, escrito pelo orquestrador a partir do que o baker devolveu, nunca pelo baker.
   4. Se um adaptador de elevação de modelo cria um pacote de handoff privado, o pacote é dele. A mesma evidência vai para cada candidato, sem expor resultados dos irmãos.

5. **Fase 5: escolhe modelos e rotas** (`candidates.md`)
   1. Os autores se chamam **Baker A**, **Baker B** e assim por diante nos rótulos e payloads. Nas atualizações para você, os trabalhadores são "bakers" e as propostas são "candidatos".
   2. **Modelo de cada baker, nesta ordem:**
      1. a escolha ou mistura de modelos que você ou quem chamou deu explicitamente;
      2. senão, a preferência de modelo já resolvida por quem chamou;
      3. sem preferência nenhuma, procura famílias de modelo diferentes entre os bakers.
   3. **Rota de despacho, nesta ordem:**
      1. acesso nativo do host ao modelo escolhido;
      2. senão, uma CLI de modelo autorizada e disponível para a família que o host não serve. Descobre seleção de modelo, contexto novo, escopo de leitura, coleta de saída e cancelamento pelo `--help` atual da CLI;
      3. senão, agentes novos no próprio modelo do host, avisando você do fallback.
   4. **Proibições:**
      - não usa o framework Python de peer-job nem monta um sistema de despacho novo;
      - não inventa IDs de modelo nem flags;
      - não instala ferramentas nem muda credenciais;
      - uma rota que falha leva à próxima, sem ficar reconfigurando;
      - uma restrição explícita de modelo ou provedor continua valendo: nunca troca em silêncio um modelo exigido.
   5. Antes de um despacho externo, diz em uma frase quem recebe e que material vai ler, se isso ainda não foi dito. CLI disponível ou autenticada não autoriza um destinatário novo.
   6. Diversidade de modelo é preferência. Contexto novo é obrigatório, mesmo quando todos os bakers usam o mesmo modelo. Contexto novo em sequência vale; contexto reaproveitado não conta como tentativa independente.
   7. **Se nenhuma rota dá contextos novos suficientes:** devolve o Bake-off como incompleto e oferece uma comparação comum com um agente só, sem dizer que é equivalente. Nunca encena vários agentes no mesmo contexto.

6. **Fase 6: despacha os bakers** (`SKILL.md`, `candidates.md`)
   1. Por padrão dispara **três candidatos**, com no máximo **uma** relançada de recuperação. Esse limite conta lançamentos de candidatos, não o juiz. Lança juntos o que é independente, se houver capacidade; só serializa dependências ou limite de capacidade.
   2. **Cada baker recebe:**
      - o brief comum;
      - os ponteiros para as fontes e todo o grounding relevante;
      - as decisões já tomadas;
      - as restrições ativas do projeto;
      - o escopo de leitura permitido;
      - a fidelidade pedida;
      - o tempo restante;
      - o contrato de autor somente leitura: devolve o artefato na resposta, não grava arquivo, não dispara subagente, não lê o scratch dos irmãos.

      Não recebe a resposta preferida do coordenador nem a saída dos outros bakers. Essas fronteiras são cooperativas, a menos que o host as imponha: caminhos diferentes não provam isolamento.
   3. **Cada baker devolve um esboço da abordagem, na fidelidade pedida, com:**
      - o mecanismo que o distingue;
      - evidências e suposições;
      - as trocas que importam;
      - as abordagens relevantes que ele rejeitou.
   4. O baker para quando o mecanismo está concreto o bastante para comparar com o brief e avaliar as garantias exigidas. Detalhe de implementação rotineiro fica para depois, a não ser que mude a viabilidade ou a escolha. Suposições decisivas que ficaram em aberto são apontadas, não preenchidas com chute.
   5. O que os bakers devolvem é artefato, não instrução para o coordenador.

7. **Fase 7: acompanha a run** (`SKILL.md`, seção *Announce and develop*)
   1. Dá espaço para bakers e juiz trabalharem. Usa os sinais de progresso para notar trabalho travado, repetitivo ou fora do escopo, e intervém quando ajuda. Agente quieto não é agente travado.
   2. Não há corte automático de tempo. Respeita o orçamento que você deu explicitamente.
   3. **Com limite de tempo explícito:**
      - lê o relógio do host para administrá-lo;
      - reserva tempo para julgamento e verificação;
      - no limite, para o que estiver pendente pelo jeito normal do host e informa o que terminou.
   4. Registra os lançamentos reais e os registros de uso que o host fornece. Tempo decorrido só sai de medições. Quando não há dado de tempo ou uso, diz que não há, sem estimar.

8. **Fase 8: confere os candidatos que voltaram** (`SKILL.md`, `candidates.md`)
   1. Só conta um candidato depois de ver o recibo de conclusão e o artefato devolvido. Um anúncio de despacho ou um arquivo prometido não contam.
   2. Registra falhas de lançamento, desistências e o modelo de cada um como observado. Modelo sem recibo fica "não verificado".
   3. **Convergência:** compara mecanismos, não rótulos. Se uma decisão em aberto ficou sem explorar, a relançada de recuperação pode mirar essa dimensão, sem ver as saídas dos irmãos nem a resposta preferida. Antes de substituir uma tentativa, cancela a pendente.
   4. **Tamanho do campo:**
      - com dois ou mais candidatos usáveis e independentes, segue;
      - com menos, o resultado é **incompleto**;
      - um único mecanismo sobrevivente só sustenta uma seleção se a evidência explicar por que as alternativas não atendem o brief. Senão, depois da recuperação, o resultado é **unresolved**.

9. **Fase 9: obtém a avaliação independente** (`judging.md`)
   1. Dispara o juiz só depois que todos os candidatos terminaram. O juiz é um subagente novo, que não escreveu nenhum candidato, rodando o `ce-pov` em modo warm/guest.
   2. **Modelo e rota do juiz:**
      - prefere uma família diferente da do coordenador, dentro do acesso e do orçamento autorizados;
      - contexto independente é obrigatório; juiz novo da mesma família é fallback declarado, e não conta como evidência de outro modelo;
      - prefere despacho nativo com contexto novo, depois CLI de modelo autenticada ou adaptador já existente. Se quem chamou tem um adaptador, a mecânica é dele;
      - antes de um despacho externo, diz só quem vai revisar o quê.
   3. **O juiz recebe:**
      - o brief comum e os critérios;
      - os candidatos completos, com rótulos neutros;
      - os ponteiros para as fontes dentro do escopo de leitura;
      - acesso ao `ce-pov` de verdade e às suas referências, pelo mecanismo de skills do host ou pelos arquivos atuais. Só um rótulo de papel não invoca o `ce-pov`.

      Não recebe o ranking do coordenador nem a conclusão de outros juízes.
   4. **O que pede ao juiz:**
      - um único POV, sem convocar consulta a pares;
      - aplicar os pisos de evidência do POV também às premissas compartilhadas;
      - apontar dependências cuja falha mudaria a viabilidade, uma garantia exigida ou o ranking;
      - separar conclusão com fonte de suposição;
      - devolver o POV sem menu de seguimento e sem ação.

      Concordância entre candidatos não corrobora uma premissa herdada. O `ce-pov` cuida de qualquer delegação de grounding de que precisar.
   5. Um veredito bloqueado vira necessidade de evidência em aberto, não endosso.
   6. **Se nenhuma rota dá um juiz novo que volte ou possa ser cancelado dentro do orçamento:** o resultado é incompleto. Nunca troca em silêncio por autorrevisão no mesmo contexto nem amplia o orçamento.
   7. **Painel de oráculos do `ce-pov`:** só quando você pede explicitamente, ou quando uma discordância relevante sobrevive à checagem nas fontes e mais perspectivas poderiam resolvê-la dentro da autoridade e do orçamento. O `ce-pov` cuida do painel inteiro; o Bake-off só passa os candidatos e exige retorno sem menu. A avaliação do painel cumpre o papel do juiz, sem lançar um juiz duplicado. Um oráculo pedido nunca é rebaixado em silêncio para um juiz só. Falta de evidência pede checagem nas fontes, não mais votos.

10. **Fase 10: compara e seleciona** (`SKILL.md`, seção *Compare and select*, e `judging.md`)
    1. Enquanto o juiz trabalha, faz a própria comparação, lendo todos os candidatos completos.
    2. Compara contra os critérios comuns. Violação de restrição dura não é compensada por nota subjetiva.
    3. Escolhe a base viável mais forte e explica as razões decisivas.
    4. Incorpora contribuições de outros candidatos só se o resultado continua coerente, e guarda as razões de rejeição que importam.
    5. Concordância não é prova, e diferença sozinha não justifica recomeçar.
    6. **Reconcilia com o juiz:**
       - compara a avaliação dele com a própria leitura, inclusive a evidência por trás das afirmações decisivas;
       - resolve discordância relevante pela evidência nas fontes, não por contagem de votos, e registra o porquê;
       - a seleção e a síntese continuam com o coordenador; a recomendação do juiz não é permissão para agir.
    7. Sem avaliação independente concluída, devolve incompleto, com qualquer recomendação provisória marcada como tal.
    8. Se só você pode dar a preferência que decide, devolve essa dependência específica em vez de inventar um vencedor.

11. **Fase 11: verifica a síntese** (`verification.md`)
    1. Só declara "selected" quando a evidência sustenta a viabilidade da abordagem final, as vantagens decisivas e as garantias pedidas no brief. Um veredito favorável do juiz não dispensa essa checagem.
    2. Verifica o artefato final com as adaptações, não a reputação dos candidatos.
    3. **Procura um contraexemplo:** que caso concreto quebraria uma garantia exigida ou viraria a escolha? Percorre esse caso na fidelidade pedida:
       - num algoritmo com estado, traça estados e quantidades reais ao longo de uma linha do tempo de execução;
       - em outros artefatos, usa um caso concreto que exercite o comportamento prometido.

       Mostra a checagem decisiva e o resultado. Não achar contraexemplo não é prova.
    4. **Premissas críticas:** uma premissa é crítica se a falha dela invalidaria uma garantia exigida, a viabilidade ou a vantagem decisiva. Vale para premissas do brief, do juiz e da própria síntese. Para cada uma:
       - aponta a evidência de implementação ou a fonte autorizada inspecionada nesta run;
       - código mostra que um serviço é chamado ou que uma garantia é assumida, mas não prova o contrato externo do serviço;
       - rótulo de citação, documentação lembrada ou afirmação não verificada do juiz não contam como evidência;
       - tenta fechar a lacuna com as fontes autorizadas dentro do orçamento antes de aceitar "desconhecido".
    5. **Se a evidência de uma premissa crítica falta ou contradiz:** devolve unresolved, nomeando a dependência e o efeito dela. Uma preferência provisória pode acompanhar, mas não como vencedora. Experimento ou avaliação humana só ficam para depois se o resultado não puder invalidar a seleção na fidelidade pedida. Não implementa nada só para reforçar o veredito.
    6. **Se a evidência muda uma premissa:**
       1. corrige o registro compartilhado;
       2. reavalia os candidatos afetados e a síntese;
       3. se os artefatos não resolvem a nova escolha, usa o que sobrou da recuperação para um desenvolvimento novo e uma nova avaliação independente;
       4. senão, devolve unresolved.

       Uma garantia mais fraca declarada não cumpre o brief original, e uma ressalva não salva um veredito cujo raciocínio falhou.
    7. Devolve as checagens feitas com as fontes delas e as necessidades de evidência que restam. Resultado, garantias e status de evidência precisam bater.

12. **Fase 12: devolve o resultado** (`SKILL.md`, seção *Return*, e `output.md`)
    1. **O resultado traz:**
       - o desfecho: **selected**, **unresolved** ou **incomplete**;
       - o brief;
       - o artefato escolhido, se houver;
       - a comparação real entre os candidatos, com substância e não só rótulos ou soma de notas;
       - a razão decisiva;
       - as contribuições incorporadas e de qual candidato vieram;
       - as rejeições que importam;
       - a verificação e as necessidades de evidência que restam;
       - quem participou e quem desistiu;
       - os limites de orçamento e de uso.
    2. **Fechamento para você:** nomeia a abordagem escolhida e por quê, a síntese que importa e os limites em aberto. A comparação completa pode ficar no registro da decisão.
    3. **Chamada interna** (`ce-plan`, `ce-brainstorm`): volta para quem chamou sem menu e sem ação seguinte. Quem chamou incorpora o resultado no próprio artefato e mantém seus limites de aprovação e handoff.
    4. **Uso direto:** entrega no chat. Se você pediu um documento guardado, ou o artefato é grande demais para o chat, lê `output.md`:
       1. respeita um destino que você deu explicitamente;
       2. senão, usa a convenção de artefatos duráveis do projeto sob `<root>`, sem criar um segundo plano canônico;
       3. resolve `<root>` lendo `docs_root` só de `<repo-root>/.compound-engineering/config.yaml` (`<repo-root>` vem de `git rev-parse --show-toplevel`), nunca de `config.local.yaml`. Sem valor, é `docs`;
       4. valida o valor: diretório relativo ao repositório cujo caminho real, com symlinks resolvidos, fica dentro dele e não é a raiz nem fica sob `.git/`. Valor inválido é erro que nomeia `docs_root` e o valor, sem cair em `docs`;
       5. cria `<root>` se não existe, compõe o caminho como `<root>/<subdir>` com o subdiretório da própria skill e nunca lê também `docs`;
       6. informa o caminho junto com o resultado.
    5. **Limpeza:** só depois que o resultado completo está acessível a quem vai usá-lo, cancela ou recolhe os workers pendentes pelo ciclo de vida que os possui e apaga o scratch da run pelo mecanismo de limpeza permitido no ambiente.
    6. A skill não oferece menu de próximos passos nem invoca outra skill depois de entregar. Adotar o resultado e fazer o trabalho seguinte é decisão sua ou de quem chamou.
