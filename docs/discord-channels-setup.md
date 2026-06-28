# Discord Channels Setup

This guide covers Discord setup for the OpenVibely `/channels` integration.

## Required Discord Bot Values

From your Discord Developer Portal application, you need:

- Bot token
- Bot user ID is discovered automatically after connection
- Optional default channel ID
- Optional free-response channel IDs

You can also seed the bot token with `DISCORD_BOT_TOKEN`; saved channel settings take over after configuration in the UI.

## Discord App Configuration

In the Discord Developer Portal:

1. Create an application and add a bot.
2. Enable the bot's `Message Content Intent`.
3. Invite the bot to the server with permissions to view channels, read message history, read messages, and send messages in the channels you want to use.
4. Copy the bot token into `System` -> `Channels` -> `Discord`.
5. Save and use `Test Connection` to verify bot authentication.

## Access Control

Discord access is project-scoped and deny-by-default.

Add authorized Discord user IDs in the Discord channel modal. If no users are configured for a project, Discord messages are rejected until at least one authorized user is added.

## Message Behavior

OpenVibely handles Discord messages through the same shared chat/task lifecycle used by Slack and Telegram:

- DMs to the bot can start project chat turns.
- Guild channel messages require a bot mention by default.
- Configured free-response channels allow messages without a bot mention.
- Bot mentions are stripped before the prompt is sent to the chat runner.
- If a chat turn is already active, additional Discord messages are queued and promoted FIFO through the shared queued-turn path.

## Attachments

Discord chat messages can include attachments. OpenVibely downloads Discord CDN attachment URLs directly, stores them with the chat turn, shows them on the Chat page, and passes supported images to the chat runner for vision-capable models.

- Up to 3 files are processed per message.
- Each file must be 10 MB or smaller.
- Images are passed as image attachments.
- Small non-image files up to 100 KB are included inline as text context.
- Larger non-image files are stored with the chat turn but are not inlined.

Attachment reading requires the same Discord gateway access as normal messages: `Message Content Intent` enabled, plus channel permissions to view the channel and read message history.

## Task Follow-Ups

Discord-origin task-thread replies use the shared task-thread queueing and steering behavior:

- Replies before the first task execution starts are durably queued and applied when execution begins.
- Replies during an active execution are steered into that run when possible.
- Replies after completion create a normal follow-up execution.
- Task goals, selected memory, lifecycle routing, cancellation, and agent resolution follow the same semantics as other channel-origin task turns.

## Troubleshooting

If the bot connects but does not respond:

1. Verify `Message Content Intent` is enabled.
2. Verify the bot has channel permissions to read and send messages.
3. Verify the Discord user ID is authorized for the selected OpenVibely project.
4. In guild channels, mention the bot unless the channel is configured as free-response.
5. Restart or save the Discord channel settings again if the bot token was rotated.
