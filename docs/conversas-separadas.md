# Conversas separadas — especificação do projeto

## Objetivo visual

- Uma conversa do WhatsApp = um grupo dedicado na lista principal do Telegram, sem tópicos e sem emoji de identificação acrescentado pela ponte.
- Grupo de contato privado: título com **nome salvo no WhatsApp** e telefone brasileiro **sem o prefixo +55**, quando disponível. Exemplo: `João (11 91234-5678)`.
- Grupo de conversa coletiva do WhatsApp: **nome do grupo original** e foto do grupo, quando acessível.
- **Limitação da interface do Telegram:** um grupo tem um único título; a ponte não consegue configurar fonte menor para colocar um número/subtítulo ao lado do nome na lista de chats. A aproximação viável é incluir ambos no título. Um grupo criado pela ponte não aparece como chat privado nativo do contato.
- Foto do contato/grupo como **foto do próprio grupo Telegram**, sem ícones ou emojis fabricados. O bot precisa ser administrador com permissão para alterar foto e título.
- Ao receber **qualquer nova mensagem** de um contato ou grupo, verificar se a foto de perfil no WhatsApp mudou; atualizar a foto do grupo Telegram **apenas se for diferente** da última versão (evitar uploads, mudanças de serviço e notificações repetitivas).
- Respeitar a privacidade do WhatsApp: foto indisponível pode impedir consulta; nesse caso manter a última foto disponível ou usar a foto neutra do Telegram.
- Nas mensagens provenientes de **grupos de WhatsApp**, indicar o remetente por nome; uma única conta/bot do Telegram não consegue simular de forma nativa o avatar de vários participantes como se fossem usuários distintos.

## Comportamento das mensagens e mídias

- Texto: encaminhar ao Telegram e permitir resposta no grupo correspondente. Não salvar cópia permanente do conteúdo no Northflank.
- Foto: baixar por uma sessão autenticada do WhatsApp, enviar ao Telegram como **foto**, preservando a prévia nativa.
- Vídeo: enviar como **vídeo**, com miniatura e reprodução via streaming quando compatível. Arquivos muito grandes ou não suportados podem exigir documento/limite e tratamento de erro.
- Documento: enviar como **documento**; incluir nome e miniatura quando Telegram/formato permitirem. Não prometer visualização prévia para todos os formatos.
- Figurinha, áudio, resposta, edição e reação: compatibilizar e testar separadamente.
- **Download automático no celular** deve seguir as configurações do **aplicativo Telegram**, não uma nova opção da ponte. Depois que o arquivo for entregue aos servidores do Telegram, o app decide se descarrega automaticamente.
- O Telegram não consegue produzir a prévia normal a partir de um endereço privado do WhatsApp sem autenticação. A ponte precisa obter o arquivo e enviá-lo ao Telegram (mesmo que remova o arquivo local logo após).
- Evitar histórico duplicado: após entrega confirmada, liberar bytes em memória e apagar arquivos temporários; em falha, reter apenas durante a tentativa/reenvio com prazo e limite.
- O WhatsApp hospeda seu próprio histórico e mídia, mas isso **não substitui** os dados mínimos da sessão criptográfica da ponte nem garante que mídias antigas ainda possam ser baixadas.

## Continuidade sem criar novas conversas

- **Não guardar histórico de mensagens no contêiner.**
- Ao criar/identificar cada conversa, registrar seu vínculo exclusivo com o grupo Telegram existente.
- A fonte **durável** do registro pode ficar no próprio Telegram (por exemplo, registro administrativo **privado**, recuperável pela sessão de usuário MTProto); isso requer implementação, autenticação e proteção contra duplicatas, e **ainda não está pronto**.
- Em reinicialização, **recuperar os vínculos persistidos antes de criar qualquer grupo**; se houver dúvida, não criar automaticamente um duplicado.
- Salvar separadamente, de modo seguro, os dados de autenticação necessários para retomar as sessões WhatsApp e Telegram. **Só os IDs dos grupos não restauram o login do WhatsApp.** Avaliar backup criptografado/armazenamento gratuito sem expor segredos em GitHub público.
- Evitar usar títulos ou fotos como única chave de associação; nomes e fotos podem mudar.

## Recursos do Northflank

- **256 MB de RAM**, limite de memória do processo. **1 GB de disco efêmero**, para temporários e bancos de trabalho; não é armazenamento garantidamente persistente após substituição do contêiner.
- Priorizar encaminhamento sequencial de mídias pesadas, limites de tamanho e limpeza mesmo após erro; testar quedas e arquivos grandes para não exceder 256 MB.
- O código atual usa `WhatsAppClient.Download(...)`, que carrega mídia como `[]byte` na RAM. Refatorar/limitar esse fluxo antes de produção.
- Foto de perfil: guardar definitivamente no Telegram como foto do grupo; não manter cópia permanente no Northflank.
- **Não gravar tokens, `api_hash`, sessões, QR, dados pessoais ou bancos SQLite no GitHub.**

## Restrições técnicas

- A Telegram Bot API **não cria novos grupos**; precisamos de uma sessão de usuário MTProto para provisioná-los automaticamente, ou criação manual.
- O bot pode atualizar nome/foto de grupos em que tenha direitos administrativos, mas **não controla a tipografia/subtítulo da lista de chats**.
- Uma conta de bot no Telegram não representa nativamente cada pessoa do WhatsApp; nomes de remetentes dentro de grupos precisam ser exibidos como texto/atribuição da mensagem.
- Não substituir a imagem do Northflank em produção antes de concluir os testes.


## Requisitos ajustados: reaproveitamento de chats, identidade e mídia

### Recuperação das conversas sem duplicar grupos

1. O identificador principal é o JID estável da conversa WhatsApp (contato/grupo). O telefone normalizado é um **identificador secundário** para contatos brasileiros, mostrado **sem +55** e sem ser a única prova de identidade. Telefones podem mudar, ser reciclados ou não estar disponíveis (LID/privacidade).
2. Cada grupo criado no Telegram deve registrar metadados técnicos da associação (JID, grupo Telegram e, quando verificável, número normalizado), preferencialmente em **descrição/registro administrativo privado no Telegram**. Não depender de nome e foto porque ambos mudam.
3. Na inicialização, a conta de usuário MTProto percorre os **grupos da ponte**, recupera e valida as associações e então mantém um índice leve em RAM. Em falta de associação durante recebimento, buscar primeiro o registro no Telegram, depois procurar telefone/nome somente como **candidato** e validar antes de reaproveitar. Se houver ambiguidade, **não criar novo grupo nem atribuir conversa errada automaticamente**.
4. Nunca varrer o histórico inteiro a cada mensagem. Usar índice e consulta pontual; permitir varredura completa de recuperação só após reiniciar/perder mapeamento. Bots via Bot API não acessam livremente histórico antigo; varredura exige **sessão de usuário MTProto**.
5. Persistir a sessão criptográfica WhatsApp e a da conta Telegram **não é o mesmo que salvar associações**. O 1 GB é efêmero: arquivos locais somem em reinicializações/substituição. Rotação local da sessão não resolve perda do disco. Estudar uma cópia **criptografada e com uma versão atual + uma cópia anterior de segurança** no armazenamento privado do Telegram, restaurada via sessão MTProto antes do login. A chave de criptografia fica fora da cópia; evitar disponibilizar credenciais ou dados em repositório público. A rotina precisa ser testada e não está implementada.
6. A recuperação de chat deve ser **idempotente**: duas mensagens simultâneas da mesma pessoa não podem criar dois grupos. Ao detectar um vínculo antigo, reaproveitar em vez de criar. Antes de apagar qualquer cópia de sessão, confirmar que a nova foi salva e pode ser restaurada.

### Aparência e cores dos participantes

- Conversas individuais: um grupo Telegram por contato WhatsApp; título legível com nome e número sem +55 quando disponível, sem emojis adicionados. Uma conversa coletiva do WhatsApp vira um grupo Telegram próprio.
- Mensagens de grupos devem exibir o nome e o telefone do **remetente real**; a mesma pessoa deve receber identificação consistente em diferentes grupos.
- **Limitação real:** o Telegram não oferece API para escolher uma cor pastel arbitrária e exclusiva para **cada número**, nem para colorir só o número dentro do título ou corpo da mensagem. Podemos manter internamente uma cor estável por contato como metadado para uso futuro, mas **não prometer exibição colorida** no app oficial. Tampouco há garantia matemática de infinitas cores distintas quando uma paleta é limitada.
- **Limitação real:** o bot não pode ocultar arbitrariamente a autoria `@Wpp_TBot` das mensagens que publica nos grupos. Mostrar nome/número no conteúdo ou legenda é possível, mas isso não o transforma em remetente nativo. Avaliar opções de apresentação permitidas pelo Telegram, sem simular identidade de terceiros.
- Fotos: usar foto WhatsApp do contato ou grupo como foto do grupo correspondente no Telegram. **A cada mensagem recebida**, conferir metadados/identificador da foto quando disponível; fazer novo download e atualização da foto **só quando a versão mudou**. Após confirmar upload ao Telegram, apagar cache temporário no servidor. Imagens devem ser reduzidas para tamanho suficiente de avatar, não para resolução original. Para abrir foto ampliada, depender da versão que o Telegram conservar ou solicitar original sob demanda quando permitido pelo WhatsApp.
- Fotos de remetentes dentro de grupos WhatsApp não se tornam automaticamente avatares distintos de cada mensagem num único bot Telegram; só é possível personalizar a foto do **grupo** e atribuir o autor no texto/legenda.

### Espaço local e política de download

- Limites do Northflank: **256 MB de RAM** para o processo, **1 GB de armazenamento efêmero** para arquivos locais.
- Proposta de orçamento do disco: até **300 MB** para banco de trabalho, índices, sessões e outros temporários pequenos; até **600 MB** para **soma dos downloads em andamento**, com ~100 MB restantes de folga. **O tamanho deve ser contabilizado antes de aceitar arquivos**, porque o espaço real disponível varia. O orçamento de 300 MB não implica guardar fotos de contatos em alta resolução; preferir fotos armazenadas pelo próprio Telegram.
- **Não iniciar download de arquivo > 600 MB.** Se a soma dos arquivos simultâneos mais o novo arquivo ultrapassar 600 MB, enfileirar ou responder: “Não foi possível baixar: arquivo maior que 600 MB ou limite de downloads simultâneos ocupado. Aguarde as transferências terminarem.”
- Para evitar gargalo em 256 MB RAM, priorizar **uma transferência por vez** e envio em partes/arquivo temporário com limites; mídia pesada não pode ficar inteira num `[]byte`. Sempre apagar o temporário após confirmação (ou falha irreversível) e liberar a reserva da fila.
- WhatsApp -> Telegram: baixar mídia **diretamente dos servidores do WhatsApp através da sessão autenticada**, nunca exigir que o WhatsApp do celular baixe antes. A API usada pode exigir download temporário e novo upload; URL privada de WhatsApp não funciona como URL pública direta no Telegram.
- **Atenção: a Bot API hospedada do Telegram limita upload normal a 50 MB**. Arquivos até 600 MB exigirão outra modalidade, por exemplo envio via **conta Telegram MTProto** (já necessária para criação dos grupos), ou servidor local Bot API. O plano grátis de 256 MB precisa de testes antes de assumir capacidade para arquivos grandes.
- Fotos: enviar como fotos com visualização. Vídeos: enviar como vídeos com miniatura/reprodução quando o formato permitir. Documentos: enviar preservando nome e possível miniatura, sem garantir preview para todos os formatos.
- Depois que a mídia chegar aos **servidores do Telegram**, respeitar download automático/cache do **aplicativo Telegram** do usuário. Não guardar cópias completas nem histórico de conteúdo no Northflank.
- Mensagens sem mídia devem ir direto sem ocupar a cota de downloads pesados. Ao usar serviço indisponível, não baixar de novo desnecessariamente nem gerar duplicatas ao fazer retry.

## Compromissos adicionais — 17/11/2026 (não esquecer)

### Integração com Northflank

- Conectar a instalação GitHub da equipe `axl64's team` ao fork `devaxl64/watgbridge-reconnect`, restringindo o acesso ao repositório específico quando possível.
- **Não mudar ainda** o serviço de produção `Default / whatsapp-telegram`: ele usa `ghcr.io/akshettrj/watgbridge:latest`. Vincular o GitHub à conta Northflank **não altera automaticamente a imagem do serviço existente**.
- Quando a nova versão estiver validada, preparar build da branch apropriada e migração controlada sem criar custos adicionais; conferir forma de desativar deploy automático durante testes e preservar variáveis/secret files.
- Versão de trabalho atual: branch `feature/separate-chats`, pull request de rascunho #1. Não mesclar nem implantar código ainda incompleto.

### Operações de mensagens exigidas

- Mensagens em duas vias: **enviar e receber textos**, fotos, vídeos, documentos, áudios e figurinhas, com respostas citadas.
- **Editar mensagens** enviadas do Telegram e refletir a edição no WhatsApp **quando o protocolo e janela de edição permitirem**; refletir alterações feitas no WhatsApp em suas cópias do Telegram. Evitar criar nova mensagem quando é possível editar a existente.
- **Apagar mensagens**: diferenciar *apagar para mim* e *apagar para todos*. Propagar exclusão/revogação quando autorizada, suportada e dentro do prazo; nunca prometer exclusão universal fora das regras das plataformas.
- Preservar vínculo mensagem WhatsApp ↔ mensagem Telegram somente enquanto necessário para editar, apagar, responder, evitar duplicações e consultar estado; preferir metadados enxutos e/ou registro privado no Telegram, em vez de armazenar conteúdo e anexos no Northflank.
- Em grupos, exibir nome/número da pessoa que escreveu; garantir associação estável entre contatos em grupos distintos. Não acrescentar emoji aos nomes. Verificar foto de perfil sempre que entrar mensagem, em privado ou grupo.

### Significado dos 600 MB

- Os **600 MB são orçamento total de espaço temporário disponível no Northflank** para transferências em andamento, **NÃO um tamanho obrigatório dos arquivos enviados pelo bot** e NÃO uma meta de enviar arquivos de 600 MB. Arquivos pequenos também são transmitidos normalmente.
- A política deve contabilizar todos os temporários ativos, não baixar mídia acima do limite individual e não ocupar a faixa de 300 MB reservada a dados de trabalho, deixando ~100 MB de folga nominal. Antes de gravar, conferir espaço real, pois parte do disco pode estar ocupada pela imagem ou outros arquivos. O código de reserva é preliminar e ainda **não está acoplado à transferência real**.
- **Modo padrão preferido:** WhatsApp (servidor) -> ponte (dados temporários) -> **servidor Telegram**. Depois do upload completo, apagar temporário imediatamente. A mensagem/mídia continuará no Telegram, mesmo se o Northflank reiniciar; o usuário pode **baixar no celular mais tarde** conforme o Telegram. Enviar à nuvem do Telegram **não significa baixar para o celular**.
- **O botão nativo de baixar do Telegram não pode iniciar, por si só, download do WhatsApp**. Para usar mídia só no WhatsApp inicialmente e transmitir depois, a ponte teria que mandar prévia/aviso mais um **botão/comando próprio “Baixar mídia”** que requisita o arquivo e faz upload naquele momento, se a mídia ainda estiver disponível no WhatsApp; isso **não é equivalente** ao botão nativo de download. Escolher a estratégia conforme requisitos/limites técnicos, sem prometer acesso direto privado de Telegram aos servidores WhatsApp.
- Foto e vídeo devem aparecer preferencialmente com prévia nativa, se upload/limite/formato permitirem; documentos com nome e miniatura quando suportado. O Telegram decide download e cache no aparelho.
- Se o envio falhar por tamanho/limites, avisar claramente, sem ocupar 600 MB indefinidamente. Respeitar a limitação de 50 MB na Bot API normal; não pressupor que o bot possa transmitir vídeo de 600 MB sem rota especial. Usar envio MTProto quando apropriado e após teste.
- Cópias de fotos de perfil devem ficar no Telegram; no Northflank só imagem miniaturizada e temporária ao comparar e atualizar. Ao abrir fotografia de perfil, depender da mídia armazenada pelo Telegram ou de solicitação sob demanda se disponível.

### Continuidade e prioridade atual

1. **PRIORIDADE ABSOLUTA AGORA:** reconstruir e persistir os vínculos com os mesmos grupos Telegram após reinicialização; jamais criar duplicatas por mero sumiço da tabela local.
2. Depois: autenticação/sessão e reconexão controlada, com no máximo um aviso por desconexão e QR apenas sob comando.
3. Depois: criação automática de grupos individuais e encaminhamento bidirecional com edição/exclusão/respostas.
4. Depois: identidade visual, avatares, nomes/números estáveis e melhorias de mídia.
5. Depois: integração segura no Northflank de produção e testes de RAM/armazenamento.
6. **Não depender da memória do usuário para relembrar estas decisões**: este documento é a fonte de requisitos do fork.

**Atenção sobre persistência:** apenas 1 GB de disco efêmero no Northflank não garante sobreviver à substituição do contêiner. Para garantir a recuperação sem um serviço pago adicional, precisamos de registro/backup seguro e durável no próprio Telegram, ou aceitar novo QR quando perder a sessão. A política “a chave nova substitui a antiga” não pode apagar a última cópia válida antes de confirmar a nova.


## Andamento

- [x] Fork de trabalho criado.
- [x] Branch `feature/separate-chats` criada.
- [x] Modelo de vínculo WhatsApp -> grupo Telegram e funções de consulta (etapa preliminar).
- [x] Testes automatizados do vínculo adicionados (GitHub Actions confirmou execução bem-sucedida em 17/11/2026).
- [x] Especificação visual e de mídia revisada.
- [ ] Definir e implementar recuperação **durável** de vínculo sem duplicação.
- [ ] Criar grupos automaticamente via MTProto, mantendo identidade visual.
- [ ] Roteamento WhatsApp -> grupo e grupo -> WhatsApp, inclusive múltiplos remetentes em grupos.
- [ ] Atualizar fotos de perfil quando houver mudança detectável.
- [ ] Enviar foto/vídeo/documento com prévias adequadas.
- [ ] Reduzir uso de RAM no fluxo de mídia.
- [ ] Persistir sessões de autenticação com segurança.
- [ ] Implementar reconexão comandada, notificação sem spam e testes no Northflank.

## Teste de rota (somente estrutura de dados)

```sh
go test ./database
```

A versão personalizada **ainda não está pronta para implantação**. O branch está isolado de `main`.
