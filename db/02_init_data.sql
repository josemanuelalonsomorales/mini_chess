INSERT INTO games (id, current_turn, status)
VALUES (1, 'WHITE', 'IN_PROGRESS')
ON CONFLICT (id) DO NOTHING;

SELECT setval(
    pg_get_serial_sequence('games', 'id'),
    COALESCE((SELECT MAX(id) FROM games), 1),
    EXISTS (SELECT 1 FROM games)
);

WITH piezas_iniciales (player, type, pos_x, pos_y) AS (
    VALUES
        ('WHITE', 'ROOK',   'A', 1),
        ('WHITE', 'BISHOP', 'B', 1),
        ('WHITE', 'KING',   'C', 1),
        ('WHITE', 'BISHOP', 'D', 1),
        ('WHITE', 'ROOK',   'E', 1),

        ('WHITE', 'PAWN',   'A', 2),
        ('WHITE', 'PAWN',   'B', 2),
        ('WHITE', 'PAWN',   'C', 2),
        ('WHITE', 'PAWN',   'D', 2),
        ('WHITE', 'PAWN',   'E', 2),

        ('BLACK', 'PAWN',   'A', 4),
        ('BLACK', 'PAWN',   'B', 4),
        ('BLACK', 'PAWN',   'C', 4),
        ('BLACK', 'PAWN',   'D', 4),
        ('BLACK', 'PAWN',   'E', 4),

        ('BLACK', 'ROOK',   'A', 5),
        ('BLACK', 'BISHOP', 'B', 5),
        ('BLACK', 'KING',   'C', 5),
        ('BLACK', 'BISHOP', 'D', 5),
        ('BLACK', 'ROOK',   'E', 5)
)
INSERT INTO pieces (game_id, player, type, pos_x, pos_y, is_alive)
SELECT 1, player, type, pos_x, pos_y, TRUE
FROM piezas_iniciales
WHERE EXISTS (
    SELECT 1 FROM games WHERE id = 1
)
AND NOT EXISTS (
    SELECT 1 FROM pieces WHERE game_id = 1
);
