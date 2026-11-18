# Próxima etapa da PR #2 — Sincronizar Status do WhatsApp com o Telegram

**Estado:** escopo registrado para desenvolvimento posterior. Não implementado ainda.
**Dependência funcional:** grupos separados e mapeamento da PR #1 (feature/separate-chats).

## Requisitos
- Capturar Status do WhatsApp de contatos (texto, foto e vídeo) e entregá-los no Telegram sem confundir com conversas privadas.
- Identificar corretamente quem publicou cada Status; preservar privacidade e aplicar `skip_status` e `status_ignored_chats`.
- Definir um destino apropriado para Status no Telegram, separado dos grupos de contatos. Validar a apresentação antes de implementar.
- Respeitar a validade dos Status (24 horas): estudar expiração/remoção no Telegram e evitar reimportação indevida ao reconectar.
- Tratar fotos e vídeos com os limites de tamanho, filas e controles de recursos existentes.
- Evitar Status duplicados após reinicialização, perda de rede e restauração de backup criptografado.
- Definir a política de respostas a Status sem confundir as mensagens com conversas de origem.
- Testes automatizados: autoria, duplicação, privacidade, expiração, mídia e reconexão.
- Teste real em Northflank antes de qualquer ativação de produção.

## Fora do escopo nesta etapa
- Publicar novos Status automaticamente em nome do usuário.
- Importar todo o histórico antigo de Status.
- Alterar o pareamento WhatsApp por código ou autenticação do Telegram por QR.

## Checklist
- [ ] Implementação
- [ ] Testes automatizados
- [ ] Validação real
- [ ] Implantação controlada

As alterações desta documentação **não mudam o comportamento do serviço atual**.
