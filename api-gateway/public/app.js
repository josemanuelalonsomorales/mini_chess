const API_URL = '/api';
const POLLING_INTERVAL_MS = 1500;
let intervalId = null;

let estadoJuego = {
  id: 1,
  turnoActual: 'WHITE',
  estado: 'IN_PROGRESS',
  piezas: []
};

let piezaSeleccionada = null;
const COLUMNAS = ['A', 'B', 'C', 'D', 'E'];

document.addEventListener('DOMContentLoaded', () => {
  obtenerEstadoJuego();
  iniciarPolling();
});

function iniciarPolling() {
  if (intervalId) clearInterval(intervalId);
  intervalId = setInterval(obtenerEstadoJuego, POLLING_INTERVAL_MS);
}

function actualizarTrackerInstancia(respuesta) {
  const instanceId = respuesta.headers.get('X-Instance-Id') || 'Gateway Directo';
  const trackerEl = document.getElementById('instanceTracker');
  if (trackerEl) {
    trackerEl.textContent = `Procesado por backend: ${instanceId}`;
  }
}

async function obtenerEstadoJuego() {
  try {
    const respuesta = await fetch(`${API_URL}/juego`);
    if (!respuesta.ok) throw new Error('Error al consultar el estado del juego');

    actualizarTrackerInstancia(respuesta);
    const data = await respuesta.json();

    estadoJuego = {
      id: data.id,
      turnoActual: data.turno_actual || data.turnoActual || 'WHITE',
      estado: data.estado || 'IN_PROGRESS',
      piezas: data.piezas || []
    };

    actualizarBannerEstado();
    renderizarTablero();
  } catch (error) {
    mostrarAlerta('Error conectando al cluster distribuido');
    console.error('Error en polling:', error);
  }
}

async function realizarMovimiento(idPieza, destinoX, destinoY) {
  try {
    const respuesta = await fetch(`${API_URL}/mover`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        id_pieza: idPieza,
        destino_x: destinoX,
        destino_y: destinoY
      })
    });

    actualizarTrackerInstancia(respuesta);

    if (!respuesta.ok) {
      const errorTexto = await respuesta.text();
      mostrarAlerta(errorTexto || 'Movimiento inválido');
      piezaSeleccionada = null;
      renderizarTablero();
      return;
    }

    piezaSeleccionada = null;
    await obtenerEstadoJuego();
  } catch (error) {
    mostrarAlerta('Error al procesar el movimiento');
    console.error(error);
  }
}

async function reiniciarPartida() {
  try {
    const respuesta = await fetch(`${API_URL}/reiniciar`, { method: 'POST' });
    actualizarTrackerInstancia(respuesta);

    if (!respuesta.ok) throw new Error('Error al reiniciar');

    piezaSeleccionada = null;
    await obtenerEstadoJuego();
    mostrarAlerta('Juego reiniciado');
  } catch (error) {
    mostrarAlerta('Error reiniciando la partida');
    console.error(error);
  }
}

function renderizarTablero() {
  const contenedorTablero = document.getElementById('chessBoard');
  if (!contenedorTablero) return;

  contenedorTablero.innerHTML = '';

  for (let fila = 5; fila >= 1; fila--) {
    COLUMNAS.forEach((columna, colIndex) => {
      const casilla = document.createElement('div');
      casilla.classList.add('tile');

      const esClara = (colIndex + fila) % 2 !== 0;
      casilla.classList.add(esClara ? 'light' : 'dark');

      casilla.dataset.posX = columna;
      casilla.dataset.posY = fila;

      const pieza = estadoJuego.piezas?.find(
        p => p.pos_x === columna &&
             String(p.pos_y) === String(fila) &&
             (p.esta_viva !== undefined ? p.esta_viva : p.estaViva)
      );

      if (pieza) {
        const elementoPieza = document.createElement('div');
        elementoPieza.classList.add('piece');

        const colorClase = pieza.jugador.toLowerCase();
        const tipoClase = pieza.tipo.toLowerCase();
        elementoPieza.classList.add(`${colorClase}-${tipoClase}`);

        casilla.appendChild(elementoPieza);
      }

      if (piezaSeleccionada && piezaSeleccionada.pos_x === columna && String(piezaSeleccionada.pos_y) === String(fila)) {
        casilla.classList.add('selected');
      }

      casilla.addEventListener('click', () => manejarClickCasilla(columna, fila));
      contenedorTablero.appendChild(casilla);
    });
  }
}

function manejarClickCasilla(posX, posY) {
  if (estadoJuego.estado !== 'IN_PROGRESS') {
    mostrarAlerta('La partida ha finalizado');
    return;
  }

  const piezaEnCasilla = estadoJuego.piezas?.find(
    p => p.pos_x === posX &&
         String(p.pos_y) === String(posY) &&
         (p.esta_viva !== undefined ? p.esta_viva : p.estaViva)
  );

  if (!piezaSeleccionada) {
    if (piezaEnCasilla) {
      if (piezaEnCasilla.jugador === estadoJuego.turnoActual) {
        piezaSeleccionada = piezaEnCasilla;
        renderizarTablero();
      } else {
        mostrarAlerta(`Es el turno de las ${estadoJuego.turnoActual === 'WHITE' ? 'Blancas' : 'Negras'}`);
      }
    }
    return;
  }

  if (piezaEnCasilla && piezaEnCasilla.id === piezaSeleccionada.id) {
    piezaSeleccionada = null;
    renderizarTablero();
    return;
  }

  if (piezaEnCasilla && piezaEnCasilla.jugador === piezaSeleccionada.jugador) {
    piezaSeleccionada = piezaEnCasilla;
    renderizarTablero();
    return;
  }

  realizarMovimiento(piezaSeleccionada.id, posX, posY);
}

function actualizarBannerEstado() {
  const elementoAlerta = document.getElementById('gameAlert');
  if (!elementoAlerta) return;

  if (piezaSeleccionada) return;

  switch (estadoJuego.estado) {
    case 'WHITE_WON':
      elementoAlerta.textContent = '¡Ganan las Blancas!';
      break;
    case 'BLACK_WON':
      elementoAlerta.textContent = '¡Ganan las Negras!';
      break;
    case 'DRAW':
      elementoAlerta.textContent = 'Tablas / Empate';
      break;
    default:
      elementoAlerta.textContent = `Turno de las ${estadoJuego.turnoActual === 'WHITE' ? 'Blancas' : 'Negras'}`;
  }
}

function mostrarAlerta(mensaje) {
  const elementoAlerta = document.getElementById('gameAlert');
  if (elementoAlerta) {
    elementoAlerta.textContent = mensaje;
  }
}
