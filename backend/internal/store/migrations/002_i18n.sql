-- lang: effective language (uz|ru|en). Follows the Telegram app language
-- until the user picks one manually (lang_manual = true).
ALTER TABLE users
    ADD COLUMN lang        TEXT    NOT NULL DEFAULT 'uz' CHECK (lang IN ('uz','ru','en')),
    ADD COLUMN lang_manual BOOLEAN NOT NULL DEFAULT false;

-- services.name stays the English name; empty translations fall back to it.
ALTER TABLE services
    ADD COLUMN name_uz TEXT NOT NULL DEFAULT '',
    ADD COLUMN name_ru TEXT NOT NULL DEFAULT '';

UPDATE services SET name_uz = 'Kuzovni yuvish',     name_ru = 'Мойка кузова'      WHERE name = 'Exterior wash';
UPDATE services SET name_uz = 'Kuzov + salon',      name_ru = 'Кузов + салон'     WHERE name = 'Exterior + interior';
UPDATE services SET name_uz = 'To‘liq detailing',   name_ru = 'Полный детейлинг'  WHERE name = 'Full detailing';
