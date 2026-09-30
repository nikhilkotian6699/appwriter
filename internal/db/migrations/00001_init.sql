-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE,
    display_name text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('admin', 'author')),
    disabled_at timestamptz,
    auth_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_settings (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    scene_token_limit integer NOT NULL DEFAULT 6000,
    autosave_snapshot_minutes integer NOT NULL DEFAULT 10,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE writers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    slug text NOT NULL,
    model_alias text NOT NULL,
    system_prompt text NOT NULL DEFAULT '',
    roles text[] NOT NULL DEFAULT '{}',
    enabled boolean NOT NULL DEFAULT true,
    temperature double precision NOT NULL DEFAULT 0.7,
    is_system boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, slug)
);
CREATE INDEX writers_user_idx ON writers (user_id);

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX projects_user_idx ON projects (user_id);

CREATE TABLE chapters (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title text NOT NULL,
    position integer NOT NULL DEFAULT 0,
    content_md text NOT NULL DEFAULT '',
    content_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chapters_project_idx ON chapters (project_id, position);

CREATE TABLE chapter_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chapter_id uuid NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('autosave', 'manual', 'pre_revision', 'pre_restore')),
    label text NOT NULL DEFAULT '',
    content_md text NOT NULL,
    content_hash text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chapter_versions_chapter_idx ON chapter_versions (chapter_id, created_at DESC);

CREATE TABLE bible_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    section text NOT NULL CHECK (section IN ('premise', 'character', 'setting', 'timeline', 'style', 'chapter_summary')),
    title text NOT NULL DEFAULT '',
    fields jsonb NOT NULL DEFAULT '{}'::jsonb,
    chapter_id uuid REFERENCES chapters(id) ON DELETE SET NULL,
    position integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bible_entries_project_idx ON bible_entries (project_id, section, position);

-- +goose Down
DROP TABLE bible_entries;
DROP TABLE chapter_versions;
DROP TABLE chapters;
DROP TABLE projects;
DROP TABLE writers;
DROP TABLE user_settings;
DROP TABLE users;
