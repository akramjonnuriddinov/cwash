CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT UNIQUE NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',   -- digits only
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX users_phone_idx ON users (right(phone, 9));

CREATE TABLE services (
    id           SERIAL PRIMARY KEY,
    name         TEXT NOT NULL,
    duration_min INT NOT NULL CHECK (duration_min > 0),
    price        INT NOT NULL DEFAULT 0,
    active       BOOLEAN NOT NULL DEFAULT true,
    sort         INT NOT NULL DEFAULT 0
);

CREATE TABLE orders (
    id           BIGSERIAL PRIMARY KEY,
    code         TEXT UNIQUE NOT NULL,          -- used in t.me/<bot>?start=<code>
    user_id      BIGINT REFERENCES users(id),   -- NULL for walk-ins not yet linked
    phone        TEXT NOT NULL DEFAULT '',
    car_plate    TEXT NOT NULL,
    service_id   INT NOT NULL REFERENCES services(id),
    status       TEXT NOT NULL CHECK (status IN ('booked','waiting','washing','ready','done','cancelled')),
    source       TEXT NOT NULL CHECK (source IN ('booking','walkin')),
    scheduled_at TIMESTAMPTZ NOT NULL,
    eta_at       TIMESTAMPTZ,
    reminded_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX orders_scheduled_idx ON orders (scheduled_at);
CREATE INDEX orders_user_idx ON orders (user_id);
CREATE INDEX orders_status_idx ON orders (status);

INSERT INTO services (name, duration_min, price, sort) VALUES
    ('Exterior wash',        30,  50000, 1),
    ('Exterior + interior',  60, 100000, 2),
    ('Full detailing',      120, 250000, 3);
