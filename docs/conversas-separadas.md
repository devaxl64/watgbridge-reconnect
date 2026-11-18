# Conversas separadas — desenvolvimento

## Objetivo

Cada conversa do WhatsApp deverá aparecer como **um grupo separado na lista principal do Telegram**, sem tópicos. A ponte receberá e responderá mensagens nesses grupos.

## Limites reais

- Um bot do Telegram **não cria grupos automaticamente** pela Bot API. Será necessário conectar uma **sessão de usuário via MTProto** para provisionar grupos ou criar os grupos manualmente.
- Os grupos não são conversas privadas nativas com os contatos do WhatsApp.
- O Northflank gratuito usado no projeto não tem volume persistente configurado. Sem persistência externa, sessões WhatsApp, vínculos de grupos e histórico podem se perder em substituições do contêiner.
- **Nunca subir tokens, `api_hash`, sessão do Telegram, QR ou bancos SQLite ao GitHub.**
- Não substituir a imagem em produção até os fluxos de envio, resposta, mídia e reconexão serem validados.

## Etapa inicial (este branch)

- [x] Criar armazenamento de vínculo WhatsApp JID -> Telegram group ID (um para um).
- [x] Consultar o vínculo em ambos os sentidos, com proteção contra associação duplicada.
- [x] Adicionar testes de vínculo e colisão.
- [ ] Criar/provisionar um grupo por chat (sessão de usuário MTProto).
- [ ] Encaminhar mensagens WhatsApp -> grupo correto.
- [ ] Encaminhar mensagens do grupo -> WhatsApp correto.
- [ ] Adaptar mídia, edição, reações e callbacks.
- [ ] Persistir a sessão do WhatsApp e a sessão MTProto sem volume pago.
- [ ] Implementar `/status`, `/reconectar` e aviso de desconexão sem spam.
- [ ] Testar memória no limite de 256 MB do Northflank.


## Decisão de recursos: 256 MB de RAM, 1 GB de disco efêmero

- Os **256 MB** são memória RAM, não armazenamento de arquivos. O contêiner tem **1 GB de disco efêmero** por padrão; arquivos nesse disco podem desaparecer quando o contêiner é substituído.
- **Persistência mínima:** manter apenas as associações `WhatsApp JID <-> Telegram group ID`, metadados de entrega necessários e **credenciais/estado de sessão WhatsApp criptografados**. Só IDs de conversa não bastam para restaurar o login do WhatsApp.
- **Sem banco de histórico:** após transmitir, manter a mensagem no Telegram; não guardar cópias de fotos, vídeos nem conteúdo textual já entregue.
- **Recepção de mídia:** baixar do WhatsApp autenticado, transmitir ao Telegram e apagar temporários imediatamente após confirmação. Não é possível simplesmente entregar uma URL privada do WhatsApp ao Telegram como se fosse um link público.
- **RAM:** o código atual usa `WhatsAppClient.Download(...)`, que devolve os arquivos como `[]byte` em memória; isso precisa ser refatorado ou limitado para não esgotar 256 MB. Usar transmissão por partes quando suportada, ou arquivos temporários com limites de tamanho, fila e limpeza em caso de erro.
- **Concorrência:** processar mídia pesada sequencialmente no plano de 256 MB, impor limites para arquivos grandes, e evitar conversões simultâneas (principalmente vídeos/stickers animados).
- **Estado externo:** buscar armazenamento gratuito e seguro para vínculos e sessões, ou mecanismo de recuperação confiável; **não** gravar sessões nem dados de contatos em repositório GitHub público.
- **Retentativas:** se o WhatsApp já tiver eliminado uma mídia, registrar falha amigável; não assumir que ela poderá ser baixada indefinidamente.

**Ordem de implementação:** primeiro os vínculos e resposta por grupo; depois reduzir a memória do fluxo de mídia, preparar persistência segura e só então substituir a versão em produção.

## Como verificar a etapa inicial

```sh
go test ./database
```

A implementação ainda **não substitui** o modelo original com tópicos. O branch está separado de `main` para não quebrar a implantação existente.
