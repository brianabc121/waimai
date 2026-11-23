-- +goose Up
-- Corrections to the app's own texts made on the platform's language page
-- (docs/I18N_DESIGN.md §8.3). They win over the texts that ship with the build.
CREATE TABLE ui_translations (
    lang       TEXT NOT NULL,
    key        TEXT NOT NULL,
    text       TEXT NOT NULL,
    -- human: written or checked by an admin; machine: filled in by the translation service
    status     TEXT NOT NULL CHECK (status IN ('human','machine')),
    updated_by BIGINT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lang, key)
);

-- +goose Down
DROP TABLE ui_translations;
