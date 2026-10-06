# ce-noslop passo a passo

O que a skill `ce-noslop` do plugin Compound Engineering faz quando é aplicada a um texto, da chamada até a entrega.
Lido do `SKILL.md` e das 2 referências de `EveryInc/compound-engineering-plugin`, v3.30.4 (commit `efcb657`, 2026-10-06). A skill não tem scripts, agentes nem assets, não dispara subagentes, não faz perguntas e não chama outra skill.
Uma versão instalada mais nova ou mais antiga pode ser diferente.

## Passos

1. **Fase 1: lê a entrada** (`SKILL.md`)
   1. O argumento é `[mode:author|edit|detect] [texto, caminho de arquivo ou nada]`.
   2. Fixa os dois objetivos, que valem juntos:
      - o texto não carrega marcas de escrita de IA e se entende na primeira leitura;
      - todo fato da fonte continua lá.

      Texto sem marcas mas ainda denso falhou. Texto simples que perdeu um qualificador também falhou.
   3. Texto de marketing para um canal específico é do `ce-promote`. A descrição da skill aponta para ele, mas ela não o chama.

2. **Fase 2: escolhe o modo** (`SKILL.md`, seção *Mode*)
   1. Se veio um token `mode:`, usa ele.
   2. Senão, decide pela entrada:
      - sem rascunho: **author**;
      - um imperativo sobre um rascunho ("deixa mais simples"): **edit**;
      - uma pergunta sobre um rascunho ("isso tem cara de IA?"): **detect**.

3. **Fase 3: escolhe o registro** (`SKILL.md`, seção *Register*)
   1. Escolhe por quem vai ler. O contrato de interação de quem chamou ganha de qualquer regra daqui.
      - **Agente falando com você:** o leitor é um colega que conhece o domínio e não viu o trabalho. Cada frase é escrita como seria dita a ele.
      - **Artefato do repositório ou do time:** neutro, no idioma do documento em volta, sem primeira pessoa e sem opinião que o artefato não precise. É o padrão quando ninguém nomeia o leitor.
      - **Texto seu:** preserva a sua voz e faz a menor edição que resolve. As edições de compreensão param em quebrar frases e devolver o ator, mantendo as suas palavras.

4. **Fase 4: lê as referências que o caso pede** (`SKILL.md`)
   1. Em **edit** e **detect**, lê `references/patterns.md`. Em **author**, só lê quando os testes sozinhos não resolvem um trecho.
   2. Para escolha de palavras, lê `references/terminology.md`:
      - deixa as frases mais fáceis sem cortar conteúdo;
      - mantém identificadores, caminhos, comandos, limites e termos técnicos exatos;
      - explica um termo técnico pouco conhecido quando o leitor precisa dele para entender ou agir;
      - troca jargão interno de fluxo de trabalho e rótulos inventados pela ação ou consequência que eles significam, salvo se quem chamou exige aquela palavra.
   3. **Texto que não está em inglês:** aplica só os testes, não o catálogo. Avisa que o catálogo não se aplicou apenas dentro dos achados do detect ou dentro da linha de mudança que quem chamou pediu, e em nenhum outro lugar.

5. **Fase 5: aplica os sete testes a cada frase** (`SKILL.md`, seção *The tests*)
   - Em author eles são restrições ao escrever; em edit e detect são checagens.
   1. **Mecanismo:** a frase diz o que a coisa faz ou como ela parece? Troca a impressão pelo fato que ela tomou o lugar, ou corta a frase.
   2. **Portabilidade:** a frase poderia ir para outro projeto sem mudar nada? Então não carrega fato sobre este.
   3. **Ator:** quem faz o verbo? Nomeia quando a fonte diz quem. Mantém a passiva quando o ator é desconhecido ou nomeá-lo não acrescenta nada.
   4. **Uma ideia:** o leitor precisaria reler para segurar a frase? Divide. Frase entendida na primeira leitura fica, por mais longa que seja.
   5. **Densidade:** um recurso isolado não prova nada. Três ou mais padrões diferentes num trecho, ou um mesmo padrão repetido em vários trechos, é achado.
   6. **Decisão primeiro:** a primeira frase traz o resultado de que o leitor precisa?
   7. **Leitor:** alguém sem o documento ou o código aberto consegue agir com isso? Explica o identificador ou nomeia a consequência.

6. **Fase 6: confere o catálogo de padrões** (`patterns.md`, em edit, detect e trechos de author que os testes não resolvem)
   1. **Piso contra falso positivo.** Um recurso só é escolha, não marca. Sinaliza quando os padrões se acumulam (teste 5). Nunca sinaliza:
      - texto entre aspas, títulos ou código;
      - um termo que o domínio usa com um sentido exato;
      - repetição deliberada para ênfase;
      - declaração de escopo, aviso de segurança ou correção real;
      - uma frase curta isolada para ênfase;
      - cabeçalho ou despedida de carta.
   2. Os números das regras são IDs estáveis: regra removida deixa buraco, nunca renumera.
   3. **Conteúdo (1 a 9):**
      1. **Puffery:** importância grampeada num fato comum ("marks a turning point", "plays a vital role"). Apaga a alegação, fica o fato.
      2. **Análise em -ing no fim:** "highlighting", "ensuring", "showcasing" explicando a importância. Apaga a oração.
      3. **Adjetivos promocionais:** "seamless", "robust", "groundbreaking". Diz a propriedade ou apaga.
      4. **Atribuição vaga:** "experts believe", "studies show". Nomeia a fonte ou corta; nunca inventa fonte.
      5. **Perspectiva de fórmula:** "despite challenges, continues to thrive", parágrafo final sobre o futuro brilhante. Fica uma limitação ou plano real e termina no último fato concreto.
      6. **Copy de setup e reviravolta:** "Ten features. Zero headaches." Diz o que a coisa faz.
      7. **Falso insight:** "what nobody tells you", "at its core". Corta a preparação e deixa a afirmação.
      8. **Metadiscurso:** "the key point is", "as you can see". Apaga; se o ponto não está claro, acrescenta apoio.
      9. **Rejeitar alternativa que ninguém levantou:** "some might say", "this isn't about X". Tira a opção falsa e diz a restrição real.
   4. **Linguagem (10 a 21):**
      10. **Vocabulário de modelo:** delve, tapestry, testament, underscore, showcase, garner, interplay, intricate, enduring, foster, crucial, pivotal, additionally, moreover, enhance, leverage, utilize, facilitate, e landscape, realm e journey como abstrações. Usa a palavra simples.
      11. **Cópula enfeitada:** "acts as", "serves as", "boasts", "represents" no lugar de "is" ou "has". Usa o verbo curto.
      12. **Não X, mas Y:** ninguém disse X. Diz Y.
      13. **Tríade forçada:** três adjetivos, exemplos ou batidas quando o natural é um ou dois. Fica só o que é verdadeiro e específico.
      14. **Rodízio de sinônimos:** a mesma coisa com três nomes. Escolhe um e repete; em texto técnico, repetição é precisão.
      15. **Intervalo que é lista:** "from onboarding to billing". Nomeia os itens.
      16. **Enchimento:** "in order to" vira "to", "due to the fact that" vira "because", "it is important to note that" some, "at this point in time" vira "now".
      17. **Ressalvas empilhadas:** "could potentially possibly" vira "may". Uma ressalva, só quando a incerteza é real.
      18. **Advérbio fazendo papel de verbo:** "runs quickly" vira "finishes in 40 ms". Troca o verbo ou dá o número.
      19. **Nominalização:** prefere o verbo ao substantivo derivado.
      20. **Metáfora emprestada para operação comum:** "vector", "surface", "primitive", "lever", "wedge", "north star", "flywheel", "evacuating". Nomeia a operação ou a coisa.
      21. **Sensação no lugar de mecanismo:** "deploys feel effortless". Diz o que o leitor pode fazer ou conferir ("a failed deploy rolls back within one minute").
   5. **Estrutura (22 a 28):**
      22. **Frase densa:** uma ideia por frase; uma cadeia de ponto e vírgula vira lista.
      23. **Passiva escondendo o ator:** "queries are validated" vira "the compiler validates queries", salvo se o ator não acrescenta nada.
      24. **Decisão enterrada:** conclusão, depois razão, depois contexto.
      25. **Título repetido na primeira frase:** apaga a repetição.
      26. **Final com resumo:** "in conclusion", "overall". Termina no último ponto concreto ou na próxima ação.
      27. **Fragmentos dramáticos:** uma fileira de frases curtas é pose. Reescreve como frases.
      28. **Dois-pontos de revelação:** dois-pontos só antes de lista ou citação.
   6. **Formatação (29 a 35):**
      29. **Travessão como muleta de ritmo:** vários por parágrafo, ou "this isn't X — it's Y". Usa ponto ou vírgula. Não mexe em travessão de código, intervalo ou tabela.
      30. **Rótulo em negrito que repete a linha:** vira prosa, salvo se a frase diz algo que o rótulo não disse.
      31. **Negrito espalhado:** negrito só no que o leitor precisa achar.
      32. **Títulos em Title Case:** sentence case.
      33. **Emoji decorativo** em títulos e bullets: remove, salvo contrato de quem chamou.
      34. **Bullets onde duas frases de prosa leem melhor**, títulos sobre seções de duas frases, linhas horizontais entre todas as seções de um documento curto. A forma segue o conteúdo.
      35. **Aspas curvas misturadas com retas:** aspas retas em texto técnico.
   7. **Marcas de chat (36 a 41):**
      36. **Abertura e fechamento de chatbot:** "Certainly", "Great question", "Let me know if you need anything else", "Want me to". Remove.
      37. **Bajulação:** "You're absolutely right" antes da resposta. Responde.
      38. **Anunciar o próximo ponto:** "Let's dive in", "without further ado". Diz o ponto.
      39. **Narração de processo num relatório:** passos que não mudam o que o leitor faz a seguir saem. Causa descartada ou correção que falhou fica, como achado.
      40. **Aviso de limite de conhecimento e preenchimento de lacuna:** "while details are limited, it likely...". Diz o que a fonte não mostra, ou corta. Nunca apresenta chute como fato.
      41. **Rigor fabricado:** contagens soltas ("resolved 11 threads"), placares, listas de tudo que foi checado. Diz o que foi decidido e por quê; se nada fora da rotina foi decidido, não diz nada.

7. **Fase 7: respeita as fronteiras** (`SKILL.md`)
   1. Nunca mexe, a menos que você aponte esse conteúdo como o que deve ser corrigido, em:
      - blocos de código;
      - texto citado;
      - frontmatter;
      - destinos de link;
      - identificadores;
      - um token exigido pelo contrato de quem chamou.
   2. Nunca diz se um texto foi escrito por um modelo.
   3. Não acrescenta nada que a fonte ou quem chamou não deu.

8. **Fase 8: entrega conforme o modo** (`SKILL.md`, seção *Mode*)
   1. **author:**
      - segura os testes enquanto quem chamou escreve e não devolve nada;
      - se recebeu conteúdo e o pedido de escrever, redige sob os mesmos testes e devolve o rascunho.
   2. **edit:**
      - reescreve só as frases em que algum teste falha e devolve o texto;
      - frase que passa fica como está, de modo que uma segunda passada no texto devolvido não muda nada;
      - só diz o que mudou, numa linha, se quem chamou pedir, e essa linha fica fora do texto reescrito e de qualquer artefato.
   3. **detect:**
      - para cada padrão encontrado, nomeia o padrão, cita a linha e dá a correção em poucas palavras;
      - não reescreve.
   4. **Arquivo:** só grava no arquivo nomeado, no lugar, quando o pedido diz isso. Senão, devolve o texto.
   5. **Pronto quando:**
      - a saída do modo foi devolvida;
      - todo fato, número, nome, citação e referência da entrada sobreviveu;
      - nada foi acrescentado além do que a fonte ou quem chamou forneceu.
   6. Não há menu nem handoff: o resultado volta para você ou para a skill que aplicou o `ce-noslop`.
