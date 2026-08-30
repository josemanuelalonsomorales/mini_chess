const simboPiezas = {
    'WHITE': { 'KING': '♔', 'ROOK': '♖', 'BISHOP': '♗', 'PAWN': '♙' },
    'BLACK': { 'KING': '♚', 'ROOK': '♜', 'BISHOP': '♝', 'PAWN': '♟' }
};

const columnas = ['A', 'B', 'C', 'D', 'E'];
let piezaSeleccionada = null;
let estadoJuego = null;

async function cargarJuego() {
    const respuesta = await fetch('/api/juego');
    if (!respuesta.ok) return;
    estadoJuego = await respuesta.json();
    renderizarTablero();
}

async function reiniciarPartida() {
    const respuesta = await fetch('/api/reiniciar', { method: 'POST' });
    if (respuesta.ok) {
        piezaSeleccionada = null;
        await cargarJuego();
    } else {
        alert("Error al reiniciar la partida");
    }
}

function renderizarTablero() {
    const contenedor = document.getElementById('tablero');
    contenedor.innerHTML = '';

    const labelTurno = document.getElementById('info-turno');
    if (estadoJuego.estado === 'WHITE_WON') {
        labelTurno.textContent = '🏆 ¡VICTORIA DE LAS BLANCAS (Jugador 1)!';
        labelTurno.style.color = '#373F68';
    } else if (estadoJuego.estado === 'BLACK_WON') {
        labelTurno.textContent = '🏆 ¡VICTORIA DE LAS NEGRAS (Jugador 2)!';
        labelTurno.style.color = '#373F68';
    } else {
        labelTurno.textContent = `Turno actual: ${estadoJuego.turno_actual === 'WHITE' ? 'BLANCAS (Jugador 1)' : 'NEGRAS (Jugador 2)'}`;
        labelTurno.style.color = '#373F68';
    }

    // Dibujamos de la fila 5 a la 1 para que las blancas queden abajo
    for (let y = 5; y >= 1; y--) {
        const coordY = document.createElement('div');
        coordY.className = 'coordenada';
        coordY.textContent = y;
        contenedor.appendChild(coordY);

        for (let xIndex = 0; xIndex < 5; xIndex++) {
            const posX = columnas[xIndex];
            const posY = y;
            const casilla = document.createElement('div');
            
            const esBlanca = (xIndex + y) % 2 === 0;
            casilla.className = `casilla ${esBlanca ? 'blanca' : 'negra'}`;
            casilla.dataset.x = posX;
            casilla.dataset.y = posY;

            const pieza = estadoJuego.piezas ? estadoJuego.piezas.find(p => p.pos_x === posX && p.pos_y === posY && p.esta_viva) : null;

            if (pieza) {
                casilla.textContent = simboPiezas[pieza.jugador][pieza.tipo];
                casilla.classList.add(pieza.jugador === 'WHITE' ? 'pieza-blanca' : 'pieza-negra');
                casilla.dataset.piezaId = pieza.id;
                casilla.dataset.jugador = pieza.jugador;
            }

            if (piezaSeleccionada && piezaSeleccionada.dataset.x === posX && piezaSeleccionada.dataset.y === parseInt(posY)) {
                casilla.classList.add('seleccionada');
            }

            casilla.addEventListener('click', () => {
                if (estadoJuego.estado !== 'IN_PROGRESS') return;
                manejarClick(casilla, pieza);
            });
            contenedor.appendChild(casilla);
        }
    }

    const esquinaVacia = document.createElement('div');
    contenedor.appendChild(esquinaVacia);

    columnas.forEach(col => {
        const coordX = document.createElement('div');
        coordX.className = 'coordenada';
        coordX.textContent = col;
        contenedor.appendChild(coordX);
    });
}

async function manejarClick(casilla, pieza) {
    const posX = casilla.dataset.x;
    const posY = parseInt(casilla.dataset.y);

    if (piezaSeleccionada) {
        const piezaId = parseInt(piezaSeleccionada.dataset.piezaId);

        if (piezaSeleccionada.dataset.x === posX && parseInt(piezaSeleccionada.dataset.y) === posY) {
            piezaSeleccionada = null;
            renderizarTablero();
            return;
        }

        const respuesta = await fetch('/api/mover', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id_pieza: piezaId,
                destino_x: posX,
                destino_y: posY
            })
        });

        if (respuesta.ok) {
            piezaSeleccionada = null;
            await cargarJuego();
        } else {
            const error = await respuesta.text();
            alert(error);
            piezaSeleccionada = null;
            renderizarTablero();
        }
    } else {
        if (pieza && pieza.jugador === estadoJuego.turno_actual) {
            piezaSeleccionada = casilla;
            renderizarTablero();
        }
    }
}

cargarJuego();