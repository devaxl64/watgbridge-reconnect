# PR #3 — Status, mídias nativas e aparência dos grupos

Escopo de uma **próxima etapa**, sem alterações de runtime por enquanto.
Dependência: PR #1 `feature/separate-chats` validada.

## Status do WhatsApp
- [ ] Receber Status de texto, fotos e vídeos no Telegram, com autor e horário.
- [ ] Separar Status das conversas privadas e grupos, com UX definida antes de implementar.
- [ ] Respeitar filtros, visibilidade, expiração de 24h, limites de mídia e proteção contra reenvio após reconexão.
- [ ] Validar respostas aos Status; não publicar Status novos automaticamente.

## Mídias como mensagens nativas
- [ ] Voz WhatsApp → Telegram: usar `sendVoice` / player nativo de mensagem de voz, em vez de `sendAudio`/música; testar áudio OGG/Opus.
- [ ] Voz Telegram → WhatsApp: preservar `PTT: true`; música e áudio normal: `PTT: false`; arquivos em `Document` continuam documentos.
- [ ] Vídeos WhatsApp ↔ Telegram: enviar como vídeo normal, respeitando tipos especiais (vídeo circular, GIF).
- [ ] Testar limites reais de upload/download, miniaturas, codec, duração e correlação de respostas.

## Nome, número e formato das mensagens em grupos
- [ ] Mostrar o número verificado do remetente na primeira linha e o nome na segunda, sem expor IDs LID como telefones.
- [ ] Testar um bloco de citação `<blockquote>` em textos, mantendo formatação HTML válida nas legendas de mídia.
- [ ] Suprimir cabeçalhos redundantes em conversas individuais; preservar menções e respostas.
- [ ] O nome do bot (por exemplo `.`) é definido no Telegram; não forçar renomeação em cada atualização.
- [ ] Reconhecer a limitação: o selo nativo de administrador não pode ser ocultado por código mantendo o bot como admin.

## Descrição e foto dos grupos
- [ ] Obter nome, imagem e descrição do grupo WhatsApp quando houver e quando a API fornecer dados.
- [ ] Mostrar descrição legível no Telegram **sem eliminar** `WATGBRIDGE-V1:`, que contém a associação criptografada.
- [ ] Refatorar a leitura/gravação do marcador com delimitadores e validação para suportar texto legível, incluindo migração de descrições antigas de forma idempotente.
- [ ] Respeitar limite de 255 caracteres da descrição do Telegram; nunca incluir a chave ou identificadores privados em texto claro sem necessidade.
- [ ] Não substituir descrição manual do usuário sem opção explícita de sincronização.

## Tópicos antigos e segurança da migração
- [ ] Identificar tópicos antigos no supergrupo original e evitar criar novos tópicos para conversas que agora usam grupos separados.
- [ ] Preservar o histórico dos tópicos antigos; não excluir grupos, sair deles nem apagar histórico automaticamente.
- [ ] Quando possível, recuperar apenas a associação entre WhatsApp e grupo dedicado, sem duplicar conversas ou misturar contatos.
- [ ] Se houver necessidade de copiar histórico, definir procedimento opt-in separado, após validar permissões e limites do Telegram.
- [ ] Não interpretar "Delete and Leave" como migração; esse botão abandona o grupo do Telegram.
- [ ] Validar grupos com IDs inválidos e tratar entradas sem número verificável.

## Critérios de conclusão
- [ ] Testes de unidade e integração.
- [ ] Teste real de voz, vídeo, Status, descrições, nomes e reconexão.
- [ ] Regressão de backup/recovery e WhatsApp por código numérico; Telegram por QR.
- [ ] Atualizar instruções de implantação no Northflank; sem deploy automático.
