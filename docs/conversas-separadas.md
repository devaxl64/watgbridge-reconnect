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

## Andamento

- [x] Fork de trabalho criado.
- [x] Branch `feature/separate-chats` criada.
- [x] Modelo de vínculo WhatsApp -> grupo Telegram e funções de consulta (etapa preliminar).
- [x] Testes automatizados do vínculo adicionados (ainda sem confirmação de execução bem-sucedida).
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
