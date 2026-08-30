CREATE TABLE games (
    id SERIAL PRIMARY KEY,
    current_turn VARCHAR(10) NOT NULL DEFAULT 'WHITE',
    status VARCHAR(20) NOT NULL DEFAULT 'IN_PROGRESS',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pieces (
    id SERIAL PRIMARY KEY,
    game_id INT NOT NULL,
    player VARCHAR(10) NOT NULL,
    type VARCHAR(20) NOT NULL,
    pos_x VARCHAR(1) NOT NULL,
    pos_y INT NOT NULL,
    is_alive BOOLEAN DEFAULT TRUE,
    FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE
);

CREATE TABLE movements (
    id SERIAL PRIMARY KEY,
    game_id INT NOT NULL,
    piece_id INT NOT NULL,
    from_x VARCHAR(1) NOT NULL,
    from_y INT NOT NULL,
    to_x VARCHAR(1) NOT NULL,
    to_y INT NOT NULL,
    moved_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
    FOREIGN KEY (piece_id) REFERENCES pieces(id)
);

INSERT INTO games (id, current_turn, status) VALUES (1, 'WHITE', 'IN_PROGRESS');

INSERT INTO pieces (game_id, player, type, pos_x, pos_y) VALUES
(1, 'WHITE', 'ROOK',   'A', 1),
(1, 'WHITE', 'BISHOP', 'B', 1),
(1, 'WHITE', 'KING',   'C', 1),
(1, 'WHITE', 'BISHOP', 'D', 1),
(1, 'WHITE', 'ROOK',   'E', 1),
(1, 'WHITE', 'PAWN',   'A', 2),
(1, 'WHITE', 'PAWN',   'B', 2),
(1, 'WHITE', 'PAWN',   'C', 2),
(1, 'WHITE', 'PAWN',   'D', 2),
(1, 'WHITE', 'PAWN',   'E', 2);

INSERT INTO pieces (game_id, player, type, pos_x, pos_y) VALUES
(1, 'BLACK', 'PAWN',   'A', 4),
(1, 'BLACK', 'PAWN',   'B', 4),
(1, 'BLACK', 'PAWN',   'C', 4),
(1, 'BLACK', 'PAWN',   'D', 4),
(1, 'BLACK', 'PAWN',   'E', 4),
(1, 'BLACK', 'ROOK',   'A', 5),
(1, 'BLACK', 'BISHOP', 'B', 5),
(1, 'BLACK', 'KING',   'C', 5),
(1, 'BLACK', 'BISHOP', 'D', 5),
(1, 'BLACK', 'ROOK',   'E', 5);