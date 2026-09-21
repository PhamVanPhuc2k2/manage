DROP TRIGGER IF EXISTS trg_conversations_updated        ON conversations;
DROP TRIGGER IF EXISTS trg_messages_touch_conversation  ON messages;
DROP TRIGGER IF EXISTS trg_messages_search              ON messages;
DROP FUNCTION IF EXISTS messages_touch_conversation();
DROP FUNCTION IF EXISTS messages_update_search();

DROP TABLE IF EXISTS message_attachments;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversation_members;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS notification_mutes;
DROP TABLE IF EXISTS notifications;

DROP TYPE IF EXISTS message_kind;
DROP TYPE IF EXISTS conversation_kind;
DROP TYPE IF EXISTS notification_type;
