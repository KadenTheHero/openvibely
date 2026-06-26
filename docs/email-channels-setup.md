# Email Channel Setup

OpenVibely can poll a dedicated email inbox with IMAP and reply by SMTP. Email messages from authorized senders are routed into the same Chat/task orchestration used by Slack and Telegram.

## Configure Email

1. Open `Channels` and choose `+ Add Channel` then `Email`.
2. Select a provider preset, such as Gmail, Outlook / Microsoft 365, Yahoo Mail, Fastmail, or iCloud Mail.
3. Enter the dedicated inbox address and app password.
4. Use `Advanced IMAP/SMTP settings` only when you need custom host or port values.
5. Add authorized senders for the current project.

If no authorized senders are configured, access is denied until senders are added. There is no pairing code or PIN exchange flow.

## Behavior

OpenVibely polls unread inbox messages, ignores self-sent messages, and skips common automated/list messages. Authorized messages become Email-origin Chat turns, and task completion or failure replies are sent back to the original email thread when `Send task completion/failure replies by email` is enabled.

Threaded replies preserve `In-Reply-To`, `References`, and a single `Re:` subject prefix. The first implementation sends plain-text UTF-8 replies.
