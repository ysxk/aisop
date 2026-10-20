CREATE TABLE admins (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    singleton boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
    username varchar(100) NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    admin_id uuid NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sku varchar(100) NOT NULL UNIQUE CHECK (length(trim(sku)) > 0),
    name varchar(200) NOT NULL CHECK (length(trim(name)) > 0),
    description text NOT NULL DEFAULT '',
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    currency varchar(3) NOT NULL DEFAULT 'CNY' CHECK (currency ~ '^[A-Z]{3}$'),
    stock bigint NOT NULL DEFAULT 0 CHECK (stock >= 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX products_status_created_idx ON products(status, created_at DESC, id DESC);
CREATE TABLE media (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('image', 'video')),
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    original_name varchar(255) NOT NULL,
    display_name varchar(255) NOT NULL,
    alt_text varchar(1000) NOT NULL DEFAULT '',
    storage_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE product_media (
    product_id uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    media_id uuid NOT NULL REFERENCES media(id) ON DELETE RESTRICT,
    position integer NOT NULL CHECK (position >= 0),
    is_cover boolean NOT NULL DEFAULT false,
    PRIMARY KEY (product_id, media_id),
    UNIQUE (product_id, position)
);
CREATE UNIQUE INDEX product_media_cover_idx ON product_media(product_id) WHERE is_cover;
CREATE INDEX product_media_media_idx ON product_media(media_id);
