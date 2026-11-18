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

## Como verificar a etapa inicial

```sh
go test ./database
```

A implementação ainda **não substitui** o modelo original com tópicos. O branch está separado de `main` para não quebrar a implantação existente.
