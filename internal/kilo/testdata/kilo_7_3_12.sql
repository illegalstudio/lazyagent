CREATE TABLE `project` (
	`id` text PRIMARY KEY,
	`directory` text NOT NULL,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL
);
CREATE TABLE `workspace` (
	`id` text PRIMARY KEY,
	`directory` text NOT NULL,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL
);
CREATE TABLE `session` (
	`id` text PRIMARY KEY,
	`project_id` text NOT NULL,
	`parent_id` text,
	`slug` text NOT NULL,
	`directory` text NOT NULL,
	`title` text NOT NULL,
	`version` text NOT NULL,
	`share_url` text,
	`summary_additions` integer,
	`summary_deletions` integer,
	`summary_files` integer,
	`summary_diffs` text,
	`revert` text,
	`permission` text,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL,
	`time_compacting` integer,
	`time_archived` integer,
	`workspace_id` text,
	`path` text,
	`agent` text,
	`model` text,
	CONSTRAINT `fk_session_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE
);
CREATE INDEX `session_project_idx` ON `session` (`project_id`);
CREATE INDEX `session_parent_idx` ON `session` (`parent_id`);
CREATE INDEX `session_workspace_idx` ON `session` (`workspace_id`);
CREATE TABLE `message` (
	`id` text PRIMARY KEY,
	`session_id` text NOT NULL,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL,
	`data` text NOT NULL,
	CONSTRAINT `fk_message_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE
);
CREATE INDEX `message_session_time_created_id_idx` ON `message` (`session_id`,`time_created`,`id`);
CREATE TABLE `part` (
	`id` text PRIMARY KEY,
	`message_id` text NOT NULL,
	`session_id` text NOT NULL,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL,
	`data` text NOT NULL,
	CONSTRAINT `fk_part_message_id_message_id_fk` FOREIGN KEY (`message_id`) REFERENCES `message`(`id`) ON DELETE CASCADE
);
CREATE INDEX `part_session_idx` ON `part` (`session_id`);
CREATE INDEX `part_message_id_id_idx` ON `part` (`message_id`,`id`);
CREATE TABLE `event_sequence` (
	`aggregate_id` text PRIMARY KEY,
	`seq` integer NOT NULL
);
CREATE TABLE `event` (
	`id` text PRIMARY KEY,
	`aggregate_id` text NOT NULL,
	`seq` integer NOT NULL,
	`type` text NOT NULL,
	`data` text NOT NULL,
	CONSTRAINT `fk_event_aggregate_id_event_sequence_aggregate_id_fk` FOREIGN KEY (`aggregate_id`) REFERENCES `event_sequence`(`aggregate_id`) ON DELETE CASCADE
);
INSERT INTO `project` (`id`, `directory`, `time_created`, `time_updated`)
VALUES ('proj_kilo', '/tmp/kilo-project', 1700000000000, 1700000003000);
INSERT INTO `workspace` (`id`, `directory`, `time_created`, `time_updated`)
VALUES ('wrk_kilo', '/tmp/kilo-project', 1700000000000, 1700000003000);
INSERT INTO `session` (
	`id`, `project_id`, `parent_id`, `slug`, `directory`, `title`, `version`,
	`share_url`, `summary_additions`, `summary_deletions`, `summary_files`,
	`summary_diffs`, `revert`, `permission`, `time_created`, `time_updated`,
	`time_compacting`, `time_archived`, `workspace_id`, `path`, `agent`, `model`
) VALUES (
	'ses_kilo_main', 'proj_kilo', NULL, 'kilo-fixture-session', '/tmp/kilo-project',
	'Kilo fixture session', '7.3.12', NULL, NULL, NULL, NULL, NULL, NULL, NULL,
	1700000000000, 1700000003000, NULL, NULL, 'wrk_kilo', '/tmp/kilo-project',
	'code', '{"id":"grok-4.3","providerID":"xai"}'
);
INSERT INTO `session` (
	`id`, `project_id`, `parent_id`, `slug`, `directory`, `title`, `version`,
	`share_url`, `summary_additions`, `summary_deletions`, `summary_files`,
	`summary_diffs`, `revert`, `permission`, `time_created`, `time_updated`,
	`time_compacting`, `time_archived`, `workspace_id`, `path`, `agent`, `model`
) VALUES (
	'ses_kilo_child', 'proj_kilo', 'ses_kilo_main', 'kilo-child-session',
	'/tmp/kilo-project', 'Kilo child session', '7.3.12', NULL, NULL, NULL,
	NULL, NULL, NULL, NULL, 1700000000500, 1700000000600, NULL, NULL,
	'wrk_kilo', '/tmp/kilo-project', 'code', '{"id":"grok-4.3","providerID":"xai"}'
);
INSERT INTO `message` (`id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('msg_kilo_user', 'ses_kilo_main', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000},"agent":"code","model":{"providerID":"xai","modelID":"grok-4.3"},"summary":{"diffs":[]}}');
INSERT INTO `part` (`id`, `message_id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('part_kilo_user_text', 'msg_kilo_user', 'ses_kilo_main', 1700000000000, 1700000000000, '{"type":"text","text":"Explain this Kilo project"}');
INSERT INTO `message` (`id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('msg_kilo_assistant_text', 'ses_kilo_main', 1700000001000, 1700000001000, '{"parentID":"msg_kilo_user","role":"assistant","mode":"code","agent":"code","path":{"cwd":"/tmp/kilo-project","root":"/tmp/kilo-project"},"cost":0.1,"tokens":{"total":70,"input":40,"output":20,"reasoning":5,"cache":{"write":1,"read":5}},"modelID":"grok-4.3","providerID":"xai","time":{"created":1700000001000,"completed":1700000001500},"finish":"stop"}');
INSERT INTO `part` (`id`, `message_id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('part_kilo_assistant_text', 'msg_kilo_assistant_text', 'ses_kilo_main', 1700000001000, 1700000001000, '{"type":"text","text":"It monitors compatible sessions.","time":{"start":1700000001200,"end":1700000001400}}');
INSERT INTO `message` (`id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('msg_kilo_assistant_tool', 'ses_kilo_main', 1700000003000, 1700000003000, '{"parentID":"msg_kilo_assistant_text","role":"assistant","mode":"code","agent":"code","path":{"cwd":"/tmp/kilo-project","root":"/tmp/kilo-project"},"cost":0.2,"tokens":{"total":35,"input":20,"output":10,"reasoning":0,"cache":{"write":1,"read":3}},"modelID":"grok-4.3","providerID":"xai","time":{"created":1700000003000},"finish":"tool-calls"}');
INSERT INTO `part` (`id`, `message_id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('part_kilo_tool', 'msg_kilo_assistant_tool', 'ses_kilo_main', 1700000003000, 1700000003000, '{"type":"tool","tool":"edit","callID":"call_kilo_edit","state":{"status":"running","input":{"path":"/tmp/kilo-project/main.go"}}}');
INSERT INTO `message` (`id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('msg_kilo_child_user', 'ses_kilo_child', 1700000000500, 1700000000500, '{"role":"user","time":{"created":1700000000500},"agent":"code","model":{"providerID":"xai","modelID":"grok-4.3"},"summary":{"diffs":[]}}');
INSERT INTO `part` (`id`, `message_id`, `session_id`, `time_created`, `time_updated`, `data`)
VALUES ('part_kilo_child_user_text', 'msg_kilo_child_user', 'ses_kilo_child', 1700000000500, 1700000000500, '{"type":"text","text":"Child prompt"}');
