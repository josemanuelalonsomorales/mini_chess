CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    rating INT NOT NULL DEFAULT 1200 CHECK (rating >= 0),
    games_played INT NOT NULL DEFAULT 0 CHECK (games_played >= 0),
    games_won INT NOT NULL DEFAULT 0 CHECK (games_won >= 0),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_games_won_valid
        CHECK (games_won <= games_played)
);

CREATE TABLE IF NOT EXISTS games (
    id SERIAL PRIMARY KEY,
    player_white_id INT REFERENCES users(id) ON DELETE SET NULL,
    player_black_id INT REFERENCES users(id) ON DELETE SET NULL,
    current_turn VARCHAR(10) NOT NULL DEFAULT 'WHITE'
        CHECK (current_turn IN ('WHITE', 'BLACK')),
    status VARCHAR(20) NOT NULL DEFAULT 'WAITING'
        CHECK (status IN ('WAITING', 'IN_PROGRESS', 'WHITE_WON', 'BLACK_WON')),
    winner_id INT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT games_players_distinct
        CHECK (
            player_white_id IS NULL
            OR player_black_id IS NULL
            OR player_white_id <> player_black_id
        )
);

CREATE TABLE IF NOT EXISTS pieces (
    id SERIAL PRIMARY KEY,
    game_id INT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    player VARCHAR(10) NOT NULL CHECK (player IN ('WHITE', 'BLACK')),
    type VARCHAR(20) NOT NULL
        CHECK (type IN ('PAWN', 'ROOK', 'BISHOP', 'KING', 'QUEEN')),
    pos_x VARCHAR(1) NOT NULL CHECK (pos_x IN ('A', 'B', 'C', 'D', 'E')),
    pos_y INT NOT NULL CHECK (pos_y BETWEEN 1 AND 5),
    is_alive BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS movements (
    id SERIAL PRIMARY KEY,
    game_id INT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    player_id INT REFERENCES users(id) ON DELETE SET NULL,
    piece_id INT REFERENCES pieces(id) ON DELETE SET NULL,
    from_x VARCHAR(1) NOT NULL CHECK (from_x IN ('A', 'B', 'C', 'D', 'E')),
    from_y INT NOT NULL CHECK (from_y BETWEEN 1 AND 5),
    to_x VARCHAR(1) NOT NULL CHECK (to_x IN ('A', 'B', 'C', 'D', 'E')),
    to_y INT NOT NULL CHECK (to_y BETWEEN 1 AND 5),
    promoted_to VARCHAR(20)
        CHECK (promoted_to IS NULL OR promoted_to IN ('QUEEN')),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_pieces_unique_live_position
    ON pieces (game_id, pos_x, pos_y)
    WHERE is_alive = TRUE;

CREATE INDEX IF NOT EXISTS idx_pieces_game_alive_id
    ON pieces (game_id, id)
    WHERE is_alive = TRUE;

CREATE INDEX IF NOT EXISTS idx_movements_game_id
    ON movements (game_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_games_player_white
    ON games (player_white_id);

CREATE INDEX IF NOT EXISTS idx_games_player_black
    ON games (player_black_id);
