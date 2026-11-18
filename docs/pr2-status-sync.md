# PR #2 — Sincronização de Status do WhatsApp

> Planejamento de próxima etapa; nenhuma alteração de runtime e nenhum deploy nesta PR inicial.
> Branch: `feature/status-sync`. Base temporária: `feature/separate-chats` (PR #1).
> Após a PR #1 ser integrada, atualizar a base para `main`.

## Objetivo

Trazer para o Telegram os **Status do WhatsApp** publicados por contatos (textos, fotos e vídeos), preservando o contexto de quem publicou e evitando notificações em excesso. A sincronização de conversas normais e os grupos separados da PR #1 devem continuar funcionando sem regressões.

## Implementação proposta (a executar na próxima etapa)

- [ ] Examinar como `whatsmeow` entrega eventos `status@broadcast`, mídias e metadados de autoria.
- [ ] Criar destino Telegram apropriado para Status, separado dos grupos de conversa individuais (decidir formato antes da implementação).
- [ ] Encaminhar Status de texto, imagem e vídeo com identificação do autor e horário; respeitar configurações de privacidade e `skip_status` e `status_ignored_chats`.
- [ ] Tratar corretamente o ciclo de vida de 24 horas dos Status, avaliando como expiração/remoção deve funcionar no Telegram, sem prometer exclusão retroativa automática.
- [ ] Evitar duplicação após reconexões, history sync, backup/restauração ou reinício do serviço.
- [ ] Aplicar os limites de mídia e fila da PR #1; avisos claros para arquivos não transferíveis; não baixar mídias desnecessariamente.
- [ ] Definir e testar tratamento das respostas aos Status, sem afetar mensagens comuns ou misturar autores.
- [ ] Verificar como o bot evita importação de Status antigos em massa na primeira vinculação.
- [ ] Cobrir testes de roteamento, mídias, duplicações e reconexão.
- [ ] Registrar instruções de teste real e ativação controlada no Northflank.

## Limites e critérios

- Esta PR só começa a ser implementada após estabilização/validação da PR #1.
- O bot não deve publicar Status novos em nome do usuário sem uma solicitação explícita.
- Evitar incluir conteúdos dos Status em descrições públicas de grupo, logs ou GitHub.
- Preservar autenticação QR da conta Telegram, pareamento WhatsApp por código numérico e backup criptografado.
- A documentação inicial não liga nenhum recurso e não altera o serviço implantado.
