CREATE TABLE categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(100) NOT NULL UNIQUE CHECK (length(trim(name)) > 0),
    description varchar(2000) NOT NULL DEFAULT '',
    sort_order bigint NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX categories_sort_idx ON categories(sort_order, name, id);
ALTER TABLE products ADD COLUMN category_id uuid REFERENCES categories(id) ON DELETE RESTRICT;
CREATE INDEX products_category_created_idx ON products(category_id, created_at DESC, id DESC);
