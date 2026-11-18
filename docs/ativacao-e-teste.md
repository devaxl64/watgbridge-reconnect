# Ativação controlada e teste real — WhatsApp ↔ Telegram

Este roteiro só deve ser executado **após** aprovação do CI e autorização do proprietário para trocar o serviço de produção. A branch de trabalho é `feature/separate-chats`, PR #1. O serviço Northflank `Default / whatsapp-telegram` usa a imagem antiga e **não deve ser interrompido antes da migração**.

## Estado de segurança

- **Sem merge e sem deploy automático** nesta etapa. A conta Telegram MTProto e a sessão WhatsApp não foram autenticadas em ambiente real de teste.
- O fork público não pode receber tokens, códigos de login, API hash, chave AES, SQLite, sessão MTProto ou QR Code.
- No Northflank, manter os valores existentes de `telegram.bot_token`, `owner_id`, `sudo_users_id` e `target_chat_id` no arquivo secreto `config.yaml`.
- Montar esse arquivo no caminho `/go/src/watgbridge/config.yaml` (diretório de trabalho do Dockerfile).
- Ativar `telegram.separate_chats: true` dentro da seção `telegram:`. Manter o bot autorizado e administrador nos grupos criados.

## Segredos de runtime necessários

Definir no Northflank, **não** como argumentos de build e **nunca** no GitHub:

- `WATG_BACKUP_KEY`: valor de 32 bytes aleatórios codificados em Base64. Gerar no dispositivo do proprietário com `openssl rand -base64 32`; guardar cópia privada e permanente. Perder a chave impede restaurar backups antigos.
- `WATG_TG_APP_ID` e `WATG_TG_APP_HASH`: credenciais da aplicação obtidas em `https://my.telegram.org/apps`, após autenticação do proprietário.
- `WATG_TG_PHONE`: número da conta pessoal do Telegram em formato internacional.
- `WATG_TG_PASSWORD`: configurar como secret somente se a conta usa senha de verificação em duas etapas.
- `WATG_AUTOCREATE_BATCH`: opcional; por padrão é **1** grupo novo por ciclo de cinco minutos. Usar **0** pausa a criação automática; o intervalo e o lote conservadores ajudam a evitar bloqueios do Telegram.
- `WATG_DEFER_PAIRING=1`: opcional para evitar geração automática de QR quando ainda não há sessão (também ocorre quando `WATG_BACKUP_KEY` está ativo).

Nenhuma chave de login deve ser enviada ao ChatGPT ou publicada num repositório.

## Deploy

1. Confirmar aprovação de `go test ./...` e `go build ./...` no GitHub Actions e o Docker build Linux AMD64 no PR #1.
2. Manter o serviço original ativo durante a preparação. Como ele usa uma imagem de terceiro, é necessário construir a branch `feature/separate-chats` pelo repositório ligado ao Northflank ou gerar uma imagem do fork antes de mudar o deploy. **Vincular GitHub sozinho não troca a imagem atual.**
3. Preservar os segredos e o arquivo `config.yaml`. Não ativar deploy automático da branch de testes sem revisar a troca de fonte.
4. Na migração autorizada, esperar uma reconexão WhatsApp por código se os dados da antiga sessão não forem migráveis. Não apagar nem revogar a sessão válida antecipadamente.

## Sequência no Telegram, após a versão de teste subir

Todos os comandos de login e descoberta devem ser enviados na **conversa privada** com `@Wpp_TBot`, pelo proprietário.

1. `/tgstatus`: conferir que a conta de usuário Telegram está configurada.
2. `/tglogin`: solicitar o QR Code temporário para autenticação MTProto; o bot entrega uma imagem na conversa privada do proprietário.
3. Abra a conversa com o bot no **computador**. No Telegram do **celular**, vá a **Configurações → Dispositivos → Conectar dispositivo** e leia o QR na tela do computador; aprove a vinculação se solicitado. **Nunca envie códigos numéricos em chats**: o Telegram invalida imediatamente códigos compartilhados em mensagens. O comando `/tgcodigo` foi desativado.
4. `/tgstatus`: confirme a autenticação da conta pessoal. Se o QR expirar, solicite um novo com `/tglogin`.
5. `/status`: consultar o WhatsApp. Se estiver sem sessão, executar `/codigo 55DDDNÚMERO` e inserir o código de pareamento no próprio WhatsApp, em **Dispositivos conectados**.
6. `/conversas`: conferir totais de conversas individuais e grupos identificados. `/conversas lista` e `/conversas lista 2` exibem páginas do **catálogo inteiro**, sem corte total de 20.
7. `/criarchats 0`: recuperar associações existentes sem criar duplicações; criação nova é gradual e automática quando `separate_chats` está ativo.
8. Verificar que chegou um documento criptografado de backup na conversa privada com legenda `WATGBRIDGE_SESSION_BACKUP_V1`, fixado. **Não remover o documento fixado nem fixar outras mensagens sobre ele**, pois a recuperação depende dele.
9. Trocar textos, respostas citadas, fotos, figurinhas, áudios, documentos e vídeos pequenos com ao menos um contato e um grupo WhatsApp reais. Confirmar que a ordenação e os avatares estão corretos.
10. Só depois de confirmar backup válido, provocar **um reinício controlado** e validar: `/status`, `/tgstatus`, ausência de QR automático, permanência dos mesmos grupos Telegram e mensagens sem duplicações.

## Limites atuais e critérios para concluir a validação

- A importação de grupos é automática e conservadora, não instantânea; não promete reconstruir chats que o WhatsApp não forneceu em metadados. Contatos identificados apenas por LID sem associação confiável com número devem aguardar, para não duplicar pessoas.
- Mídias grandes acima do limite da Bot API hospedada (normalmente 50 MB) não serão entregues como arquivo Telegram sem implementação de envio alternativo via MTProto ou Bot API local. **600 MiB** é orçamento de disco temporário, não limite autorizado de upload na Bot API.
- O backup de sessão com Bot API suporta arquivo criptografado até 18 MiB, e cada SQLite é atualmente limitado a 12 MiB antes da compressão. Validar tamanhos reais no serviço. Não prometer recuperação quando a cópia estiver ausente ou a sessão tiver sido revogada pelo WhatsApp.
- A sincronização de mensagens e mídia ainda requer teste real, inclusive restabelecimento após perda de sessão, limites da conta Telegram e regras de edição/exclusão.
- Manter produção anterior até os testes serem concluídos; **sem autorização explícita, não fazer merge nem deploy**.
