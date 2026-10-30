INSERT INTO games (id, current_turn, status) 
VALUES (1, 'WHITE', 'IN_PROGRESS') 
ON CONFLICT (id) DO NOTHING;

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
(1, 'WHITE', 'PAWN',   'E', 2),
(1, 'BLACK', 'PAWN',   'A', 4),
(1, 'BLACK', 'PAWN',   'B', 4),
(1, 'BLACK', 'PAWN',   'C', 4),
(1, 'BLACK', 'PAWN',   'D', 4),
(1, 'BLACK', 'PAWN',   'E', 4),
(1, 'BLACK', 'ROOK',   'A', 5),
(1, 'BLACK', 'BISHOP', 'B', 5),
(1, 'BLACK', 'KING',   'C', 5),
(1, 'BLACK', 'BISHOP', 'D', 5),
(1, 'BLACK', 'ROOK',   'E', 5)
ON CONFLICT DO NOTHING;
