# crew: kit de marketing

2026-10-08 · Matheus

**Seu expediente acaba. O do seu time, não.** O crew dá a quem trabalha sozinho um time que continua trabalhando quando a pessoa para. Você passa o dia no que só você faz; à noite, o time faz o resto, passando pelos portões de qualidade que você definiu.

## A história: o gargalo saiu de você

Antes, o limite do trabalho era o seu tempo. Agora, é o limite da sua assinatura. Essa é a história do lançamento: verdadeira, sua, e qualquer pessoa entende sem saber nada de ferramenta.

|  | Sozinho | Sozinho, com um crew |
| --- | --- | --- |
| O gargalo | Você | O limite da assinatura |
| Seu tempo vai para | Abrir sessões, passar tarefas, conferir | Escrever o que quer, pensar, decidir |
| Horas de trabalho por dia | As suas | 24, sete dias por semana |
| Quando você fecha o laptop | Tudo para | O time começa |

**O dia de quem usa:** durante o expediente, você faz o que depende de você: escreve as demandas, faz os brainstorms, toma as decisões. Quando encerra, liga o crew. O time trabalha a noite toda, e de manhã o trabalho está adiantado.

**Por que dá para dormir tranquilo:** os portões de qualidade. Nada avança de uma etapa para a outra sem passar pelas verificações que você definiu. É a resposta para quem diz que agente sem supervisão produz lixo. E cada portão que você melhora deixa o time mais confiável na noite seguinte.

**A consequência para o produto:** se o novo gargalo é a assinatura, gastar menos tokens é a funcionalidade número 1. Cada token economizado é trabalho a mais pela mesma assinatura.

## Posicionamento

**Tagline:** *Your workday ends. Your crew's doesn't.*

**Em português:** *Seu expediente acaba. O do seu time, não.*

**Em uma linha (bio, descrição do repositório, card):** *crew gives people who work alone a team of AI agents that keeps working after they stop.*

**O papel da pessoa:** você faz o que depende de você (pensar, escrever, decidir), e o time faz o resto. Sem "chefe", sem "gestor", sem "comandar".

**O time é o meio, não a mensagem:** a pessoa não quer um time; quer o tempo de volta e o trabalho andando. O time é como isso acontece.

**Para quem é:**

- Quem trabalha sozinho e tem mais trabalho do que horas.
- Quem já usa agentes de IA e sente que eles param quando a pessoa para.
- Quem se sente em casa num terminal.

**Para quem não é:**

- Quem quer um agente que decide o que fazer: no crew, o que fazer é sempre você que escreve.
- Quem não quer definir as próprias etapas e verificações.

## Mensagens-chave

Cinco pilares, todos sem jargão. Cada post ou seção do README usa pelo menos um, com a prova ao lado.

| Pilar | A mensagem | A prova |
| --- | --- | --- |
| Trabalha quando você para | Você fecha o laptop, o time começa. 24 horas, sete dias. | 39% dos commits do próprio crew desde 30/09 são do time do crew; medir as horas da noite |
| Seu tempo no que é seu | Você escreve, pensa e decide. Abrir sessão e passar tarefa sai da sua lista. | O trabalho anda sozinho de uma etapa para a outra; um painel ao vivo mostra quem faz o quê |
| Portões de qualidade | Nada avança sem passar pelas verificações que você definiu. Por isso dá para dormir. | Verificações entre etapas; agentes que perguntam e esperam a resposta quando têm dúvida |
| Qualquer trabalho com etapas | Código, texto, tradução, pesquisa: as etapas são suas. | Nenhuma etapa nem nome de fábrica |
| Mais trabalho pela mesma assinatura | Roda na sua máquina, com a assinatura que você já paga, gastando o mínimo de tokens. | Binário único, código aberto (MIT); uso medido em cada sessão |

**O que nunca dizer:**

- "Chefe", "boss", "gestor", "palavra final": a pessoa não quer virar gerente de robô.
- "Autônomo", "substitui pessoas": promete demais e gera desconfiança.
- Jargão na primeira tela: issue, PR, label, worktree.

## Exemplos de times

A mesma divisão em qualquer trabalho: de dia, você faz o que só você faz; de noite, o time faz o resto. Cada linha pode virar um processo de exemplo no repositório.

| Time | O que você faz de dia | O que o time faz de noite | Portão de qualidade |
| --- | --- | --- | --- |
| Software | Escreve as demandas, faz os brainstorms | Planeja, constrói, testa, revisa | Testes e revisão passando antes de chegar a você |
| Conteúdo | Escolhe as pautas | Pesquisa, rascunha, revisa o estilo | Fontes checadas, guia de estilo cumprido |
| Tradução | Define o glossário | Traduz, revisa, prepara para publicar | Glossário respeitado, nada sem tradução |
| Pesquisa | Faz as perguntas | Junta fontes, sintetiza, escreve o relatório | Toda afirmação com fonte |

O time de software é o que você usa hoje. O de tradução já existe nos testes do crew. Os dois bastam para o lançamento.

## Contra as alternativas

As outras opções fazem uma tarefa quando você pede. O crew é a única que continua trabalhando, etapa após etapa, depois que você sai.

|  | crew | Agentes de código (Copilot, Devin) | Automação (n8n, Zapier) | Frameworks de agentes (CrewAI, LangGraph) |
| --- | --- | --- | --- | --- |
| O que você ganha | Um time que trabalha quando você para | Um agente para uma tarefa | Uma sequência de passos | Peças para programar um time |
| Trabalha a noite toda sozinho | Sim, de etapa em etapa | Uma tarefa por vez | Se você montar | Se você programar |
| Quem define as etapas | Você, num arquivo | O fornecedor | Você, num editor visual | Você, programando |
| Cada etapa é | Um agente que lê e escreve arquivos | O mesmo agente | Uma chamada de API | Uma chamada de modelo |
| Serve para | Qualquer trabalho com etapas | Código | Integrações | O que você construir |
| Custo extra | Nenhum: usa sua assinatura | Assinatura e uso | Plano e execuções | Tokens de API |

## Nova abertura do README

Entra no lugar do primeiro parágrafo do README. A parte técnica desce para "How it works". A única menção a ferramentas específicas é a linha "Today", no fim.

```markdown
# crew

**Your workday ends. Your crew's doesn't.**

crew gives people who work alone a team of AI agents that keeps working
after they stop. Spend your day on what only you can do. When you log off,
start your crew. In the morning, the work has moved.

![one night of a crew at work, in 30 seconds](docs/night.gif)

Working alone used to mean your time was the limit. With a crew, the limit
is your AI subscription: work goes on at night, on weekends, while you
sleep.

- **It works when you don't.** Close the laptop; your crew picks up
  where you left off and moves the work from step to step.
- **Your day goes to your part.** You write, think and decide. Starting
  sessions and handing out tasks is off your list.
- **Quality gates let you sleep.** Nothing moves to the next step until
  it passes the checks you wrote. Better gates, better nights.
- **Any work with steps.** Code, writing, translation, research:
  the steps are yours, nothing is built in.
- **More work from the same subscription.** crew runs on your machine,
  with the AI subscription you already pay for, and reports what each
  piece of work used. One binary, MIT licensed.

## Is crew for you?

Yes, if you work alone, have more work than hours, and feel at home in a
terminal.

Not yet, if you want an agent that decides what to do. With crew, what to
do is always yours to write.

## Why not something else?

Coding agents and automation tools do a task when you ask. Agent
frameworks make you program the team. crew keeps working, step after step,
after you've gone.

Today crew takes its work from GitHub and runs Claude Code or Codex.
Both plug in: crew's core knows neither.
```

## A demo: uma noite em 30 segundos

Um time-lapse de uma noite real, sem som. A imagem é o relógio correndo e o trabalho andando sem ninguém.

1. **0 a 4 s.** 19h. Você escreve duas demandas e fecha o laptop. Legenda: *7 pm. You log off.*
2. **4 a 20 s.** O painel ao vivo do crew, com um relógio no canto passando de 19h a 7h. Os trabalhos mudam de etapa; os membros do time aparecem com nome e avatar. Legenda: *Your crew doesn't.*
3. **20 a 26 s.** 7h. Você abre o laptop: as duas demandas prontas para você olhar, com tudo verificado. Legenda: *7 am. The work has moved.*
4. **26 a 30 s.** Um número na tela: "12 hours of work. Same subscription." Tela final: *crew — your workday ends, your crew's doesn't.*

**Como gravar:** deixe o crew rodando uma noite com captura de tela a cada minuto, e monte o time-lapse. Use uma noite com um trabalho de código e um de texto, para mostrar que não é só código. Grave também uma versão narrada de 2 minutos.

## Posts de lançamento

Todo post conta a mesma história: o gargalo saiu de mim e foi para a assinatura. Muda só o quanto de técnica aparece. Troque o que está entre colchetes por números reais.

### Show HN (inglês)

**Título:** Show HN: crew – my workday ends, my AI crew's doesn't

```text
I work alone. For years my limit was my own time. Now it's my AI
subscription: I spend the day writing what I want and making decisions,
and when I log off I start crew. It works through the night.

crew is a single Go binary. You describe your steps once in a YAML file:
which agent does each step, and which checks must pass before work moves
on. crew watches your work queue and moves each piece through those steps,
one agent per step, each in its own workspace and with its own identity.

What makes it safe to leave running:
- Quality gates between steps. Nothing moves on until it passes the
  checks I wrote. Better gates, better nights.
- No LLM in the coordination. Steps and routes are deterministic; only the
  steps' work is done by agents.
- An agent that isn't sure asks a question and waits for the answer.

Numbers from my own use: [X]% of my subscription used per week,
[N] pieces of work finished overnight, 39% of crew's own commits since
Sep 30 made by its crew.

Nothing in it is about code: I also use it for [writing / translation].
Today the queue is GitHub and the agents are Claude Code or Codex; both
are adapters, so others can plug in.

What isn't good yet: [be honest].

https://github.com/thatsnotmynameio/crew
```

### Reddit r/ClaudeAI (inglês)

**Título:** My bottleneck used to be me. Now it's my Max subscription limit.

```text
Solo here. I spend my day writing what I want and making decisions. When I
log off, I start crew and it works through the night: one Claude Code
session per step, with checks between steps so nothing half-done moves on.

I now hit [X]% of my weekly limit, because work goes on 24/7.

[time-lapse GIF]

Open source (MIT), runs locally on the subscription you already have.
Happy to share my steps and gates.
```

### Thread no X ou Bluesky (inglês)

1. My workday ends. My crew's doesn't. [time-lapse]
2. Working alone, my limit was always my time.
3. Now I spend the day on my part: writing what I want, thinking, deciding.
4. When I log off, I start crew. A team of AI agents works through the night, step by step.
5. Quality gates between steps are what let me sleep. Nothing half-done moves on.
6. My bottleneck isn't me anymore. It's my subscription limit.
7. Open source, runs on the subscription you already pay: [link]

### LinkedIn (português)

```text
Meu expediente acaba. O do meu time, não.

Trabalho sozinho, e por muito tempo o meu limite foi o meu tempo.
Hoje é o limite da minha assinatura de IA.

Durante o dia, faço o que depende de mim: escrevo as demandas, penso,
decido. Quando encerro, ligo o crew, uma ferramenta open source que
construí. Um time de agentes trabalha a noite toda, etapa por etapa.

O que deixa isso seguro são os portões de qualidade: nada avança sem
passar pelas verificações que eu defini. Quanto melhores os portões,
melhores as noites.

O gargalo saiu de mim. Agora o desafio é fazer mais com a mesma
assinatura.

O código está aberto: [link]
```

## Artigo de portfólio

Um texto longo, em inglês, linkado no README. Mostra como você pensa, não só o que fez.

**Título:** *My workday ends. My crew's doesn't.*

**Subtítulo:** *How I moved my bottleneck from my own time to my AI subscription.*

1. **O gargalo era eu.** Trabalhando sozinho, tudo dependia das minhas horas, inclusive o trabalho que não precisava de mim.
2. **A divisão.** O que só eu faço (escrever o que quero, pensar, decidir) e o que um time pode fazer.
3. **Os portões de qualidade.** Por que são eles, e não o modelo, que permitem deixar o time trabalhando sozinho. Como cada portão melhorado rende uma noite melhor.
4. **Nenhuma IA na coordenação.** Por que etapas determinísticas deixam a noite previsível.
5. **A arquitetura.** O núcleo puro, sem I/O e sem relógio; o quadro e os agentes como encaixes; o journal que deixa o trabalho continuar depois de uma queda. Um diagrama.
6. **O novo gargalo.** A assinatura. Os números de uso e o que fiz para render mais com os mesmos tokens.
7. **O que deu errado.** Duas ou três noites que não saíram como eu queria, e o portão que nasceu de cada uma.
8. **Além do código.** O mesmo time para texto e tradução.

**Tamanho:** de 1.500 a 2.500 palavras, com o time-lapse no topo e o diagrama no meio.

## Plano de lançamento

Três fases em cerca de 4 semanas. A semana de medição vem primeiro, porque os números dela alimentam todos os posts.

**Antes (semanas 1 e 2)**

- [ ] Medir uma semana: trabalho feito por hora do dia, quanto do limite da assinatura foi usado, quantos trabalhos terminaram fora do expediente
- [ ] Gravar o time-lapse de uma noite e o vídeo narrado de 2 minutos
- [ ] Trocar a abertura do README pela desta página
- [ ] Mostrar os tokens usados por trabalho, e um total por noite
- [ ] Publicar dois processos prontos para copiar, com seus portões: software e um que não é código
- [ ] Um `crew init` que cria um processo inicial e roda em 5 minutos
- [ ] Instalação em uma linha
- [ ] Imagem de preview social com *Your workday ends. Your crew's doesn't.*
- [ ] Pedir para 3 pessoas que trabalham sozinhas deixarem o crew rodando uma noite, e anotar o que encontraram de manhã

**No dia (semana 3)**

- [ ] Show HN numa terça ou quarta de manhã (horário de Nova York), e 4 horas respondendo
- [ ] No dia seguinte: r/ClaudeAI, thread no X ou Bluesky e LinkedIn

**Depois (semana 4 em diante)**

- [ ] Publicar o artigo de portfólio
- [ ] Responder toda contribuição de fora em até 48 horas
- [ ] Um post por semana em r/SideProject, r/opensource e r/golang, cada um com seu ângulo (o r/golang quer a arquitetura)
- [ ] Enviar para newsletters: Golang Weekly, TLDR, Console.dev

## Como saber se deu certo

Metas para 90 dias depois do Show HN. O sinal mais forte é outra pessoa contando que deixou o crew rodando de noite.

| Sinal | Meta | Como medir |
| --- | --- | --- |
| Pessoas de fora deixando o crew rodar de noite | 10 | Relatos, perguntas, processos compartilhados |
| Alguém usando fora de código | 2 | Processos de texto, tradução ou pesquisa |
| Estrelas no repositório | 300 | Página do repositório |
| Contribuições de fora | 3 | Mudanças enviadas por outras pessoas |
| Tempo até a primeira noite com o crew | menos de 30 minutos | Observar as 3 pessoas do teste |

**Se não bater:** com menos de 3 pessoas de fora em 90 dias, o crew fica como a sua ferramenta e a sua peça de portfólio. Já é um bom resultado: ele continua trabalhando para você toda noite.

## Fontes

Da pesquisa desta conversa. Os números de mercado vêm de blogs e de fornecedores, e nenhum foi auditado.

- [OpenAI: Symphony](https://openai.com/index/open-source-codex-orchestration-symphony)
- [Copilot coding agent: issues virando PRs](https://noqta.tn/en/blog/github-copilot-coding-agent-autonomous-pr-issues-2026)
- [Devin: 67% de PRs aceitos](https://agentmarketcap.ai/blog/2026/04/07/devin-67-percent-pr-merge-rate-autonomous-coding-agent-performance)
- [Preços: Devin vs Copilot](https://www.costbench.com/compare/devin-ai-vs-github-copilot/)
- [Greptile: a ascensão dos agentes noturnos](https://greptile.com/blog/rise-of-the-overnight-agents)
- [AgentConn: PRs de agentes](https://agentconn.com/blog/four-horsemen-agentic-coding-agent-prs-2026)
- [Ask HN: flow com agentes](https://hn.svelte.dev/item/47797632)
- [Anthropic: Stop babysitting your agents](https://claude.com/code-with-claude/session/ldn-stop-babysitting-your-agents)
- [Spec-driven development ou cascata 2.0?](https://alexcloudstar.com/blog/spec-driven-development-2026/)
- [pikehouse/crew, o outro projeto com o mesmo nome](https://github.com/pikehouse/crew)
