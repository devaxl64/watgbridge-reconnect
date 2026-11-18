# Recuperação automática do WhatsApp (sem notebook)

## Esta implementação já foi programada, mas não instalada

Branch `feature/session-recovery` — independente do trabalho futuro em `feature/separate-chats`.

O serviço de produção no Northflank (`Default -> whatsapp-telegram`) ainda usa a imagem antiga. **Não alterar até o teste de compilação e o planejamento de migração estarem concluídos.**

## Como funciona

1. Com sessão autenticada, a ponte cria uma cópia consistente de `wawebstore.db` e `gobot.sqlite.db`.
2. Comprime e criptografa ambas usando **AES-256-GCM**, com chave aleatória de 32 bytes guardada **apenas no ambiente secreto do Northflank**.
3. Envia o backup criptografado como documento `watgbridge-session.enc` para o **chat privado com o bot** e fixa essa mensagem. Cópias seguintes atualizam essa mensagem sempre que possível.
4. Ao reiniciar, antes de abrir o banco SQLite, o programa recupera o documento fixado, verifica criptografia e restaura somente os bancos que estiverem faltando.
5. A primeira cópia ocorre cerca de 30 segundos depois de o WhatsApp estar autenticado; em seguida, atualiza de 30 em 30 minutos. Uma falha temporária é repetida depois de 5 minutos sem spam.

**Não é uma garantia de recuperação:** o WhatsApp pode revogar sessões; snapshots podem ficar desatualizados; a conta do Telegram pode não estar disponível. O arquivo criptografado atual tem teto de 18 MiB, por causa do limite de download de 20 MB da Telegram Bot API.

## Configuração no Northflank (quando for instalar)

No Linux, gerar uma chave aleatória, sem publicá-la:

```sh
openssl rand -base64 32
```

No Northflank:

```text
axl64's team -> Projects -> Default -> Services -> whatsapp-telegram
  -> Configure -> Environment
  -> Runtime variables
  -> Add variable

Key: WATG_BACKUP_KEY
Value: colar a chave gerada
Secret: habilitar, se disponível

Save
```

**Não** colocar essa chave no GitHub, no `config.yaml` versionado, em screenshots ou em mensagens do Telegram. O token do bot e os IDs já existem na configuração do serviço. A chave **não está no repositório**. Se a chave se perder, não será possível decifrar os backups antigos.

O backup aparece como documento fixado no chat privado `@Wpp_TBot`. Não apague ou desafixe esse documento. Após o primeiro backup, confirmar que ele apareceu e que os logs não registram erros de backup. **Um commit no GitHub não instala o programa no Northflank automaticamente.**

## Reconexão pelo mesmo celular

Se um reinício apagou o banco antes do backup ou o WhatsApp revogou a sessão, a ponte mantém o Telegram ativo, sem disparar novos QR Codes.

Comandos na conversa privada de `@Wpp_TBot`:

- `/status` — estado da conexão WhatsApp.
- `/reconectar` — tenta reabrir sessão já existente.
- `/codigo 5511941233764` — gera código de vinculação do WhatsApp para a conta principal.

No celular: `WhatsApp -> Dispositivos conectados -> Conectar dispositivo -> Conectar com número de telefone` e digitar o código recebido. O código expira rapidamente e deve ser mantido privado.

Se houver uma sessão válida, o comando `/codigo` não cria outra. No modo criptografado, `/newqr` não apaga a sessão sem necessidade.

## Segurança e limitações

- O banco WhatsApp **contém chaves de login**. Não é cache descartável.
- Os 256 MB do Northflank são RAM e o 1 GB é disco efêmero; o backup fica no servidor do Telegram, não nesse disco.
- Só a cópia criptografada deve ir ao Telegram; nenhuma chave de descriptografia.
- O backup inclui a tabela de vínculos do WATGBridge, mas há intervalo entre cópias: vínculos criados depois da última cópia podem se perder. A recuperação dos próprios grupos Telegram, sem duplicação mesmo nessa situação, é projeto separado e **ainda está em desenvolvimento**.
- **Não** subir banco SQLite, chave, token ou informações pessoais ao GitHub.
- Ainda não foi feito teste real no seu WhatsApp. A automação e a compatibilidade precisam ser validadas antes de alterar o serviço de produção.
