
const API_URL = '/api';
const POLLING_INTERVAL_MS = 1500;
const REQUEST_TIMEOUT_MS = 8000;

const AUTH_TOKEN_KEY = 'miniChessAccessToken';
const AUTH_USER_KEY = 'miniChessUser';

let intervalId = null;
let consultaEnCurso = false;
let consultaPendiente = false;
let accionEnCurso = false;
let versionEstado = 0;
let alertaPersistente = false;
let errorConexionMostrado = false;

let accessToken = null;
let usuarioActual = null;

let estadoJuego = {
  id: 1,
  turnoActual: 'WHITE',
  estado: 'IN_PROGRESS',
  piezas: []
};

let piezaSeleccionada = null;

const COLUMNAS = ['A', 'B', 'C', 'D', 'E'];
const JUGADORES_VALIDOS = ['WHITE', 'BLACK'];

const TIPOS_VALIDOS = [
  'PAWN',
  'ROOK',
  'BISHOP',
  'KING',
  'QUEEN'
];

const ESTADOS_VALIDOS = [
  'IN_PROGRESS',
  'WAITING',
  'WHITE_WON',
  'BLACK_WON',
  'DRAW'
];

// Inicializa la interfaz, restaura la sesión y comienza la sincronización.
document.addEventListener('DOMContentLoaded', () => {
  inicializarAutenticacion();
  obtenerEstadoJuego();
  iniciarPolling();
});

// =========================================
// AUTENTICACIÓN
// =========================================

// Recupera la sesión guardada en la pestaña actual.
function inicializarAutenticacion() {
  try {
    accessToken = sessionStorage.getItem(AUTH_TOKEN_KEY);

    const usuarioGuardado = sessionStorage.getItem(AUTH_USER_KEY);

    usuarioActual = usuarioGuardado
      ? JSON.parse(usuarioGuardado)
      : null;

    if (
      !accessToken ||
      !usuarioActual ||
      typeof usuarioActual.username !== 'string'
    ) {
      limpiarSesionLocal();
    }
  } catch (error) {
    console.error('No se pudo restaurar la sesión');
    limpiarSesionLocal();
  }

  actualizarInterfazAutenticacion();

  const loginForm = document.getElementById('loginForm');
  const registerForm = document.getElementById('registerForm');
  const toggleButton = document.getElementById('toggleAuthMode');
  const logoutButton = document.getElementById('logoutButton');

  if (loginForm) {
    loginForm.addEventListener('submit', iniciarSesion);
  }

  if (registerForm) {
    registerForm.addEventListener('submit', registrarUsuario);
  }

  if (toggleButton) {
    toggleButton.addEventListener('click', alternarFormularioAuth);
  }

  if (logoutButton) {
    logoutButton.addEventListener('click', cerrarSesion);
  }
}

// Actualiza los elementos visibles según el estado de autenticación.
function actualizarInterfazAutenticacion() {
  const authSection = document.getElementById('authSection');
  const sessionSection = document.getElementById('sessionSection');
  const sessionUsername = document.getElementById('sessionUsername');

  const sesionActiva = Boolean(accessToken && usuarioActual);

  if (authSection) {
    authSection.hidden = sesionActiva;
  }

  if (sessionSection) {
    sessionSection.hidden = !sesionActiva;
  }

  if (sessionUsername && sesionActiva) {
    sessionUsername.textContent = usuarioActual.username;
  }
}

// Guarda el token y los datos públicos del usuario en la sesión de la pestaña.
function guardarSesion(token, usuario) {
  if (!token || !usuario || !usuario.username) {
    throw new Error('El servidor devolvió datos de sesión incompletos');
  }

  try {
    sessionStorage.setItem(AUTH_TOKEN_KEY, token);
    sessionStorage.setItem(AUTH_USER_KEY, JSON.stringify(usuario));
  } catch (error) {
    limpiarSesionLocal();
    throw new Error('No se pudo guardar la sesión en este navegador');
  }

  accessToken = token;
  usuarioActual = usuario;

  actualizarInterfazAutenticacion();
}

// Elimina las credenciales locales.
function limpiarSesionLocal() {
  try {
    sessionStorage.removeItem(AUTH_TOKEN_KEY);
    sessionStorage.removeItem(AUTH_USER_KEY);
  } catch (error) {
    // La sesión local se considera cerrada aunque el almacenamiento falle.
  }

  accessToken = null;
  usuarioActual = null;
}

// Alterna entre los formularios de registro e inicio de sesión.
function alternarFormularioAuth() {
  const loginForm = document.getElementById('loginForm');
  const registerForm = document.getElementById('registerForm');
  const authTitle = document.getElementById('authTitle');
  const toggleButton = document.getElementById('toggleAuthMode');

  if (!loginForm || !registerForm) {
    return;
  }

  const mostrarRegistro = loginForm.hidden === false;

  loginForm.hidden = mostrarRegistro;
  registerForm.hidden = !mostrarRegistro;

  if (authTitle) {
    authTitle.textContent = mostrarRegistro
      ? 'Crear cuenta'
      : 'Iniciar sesión';
  }

  if (toggleButton) {
    toggleButton.textContent = mostrarRegistro
      ? '¿Ya tienes cuenta? Inicia sesión'
      : '¿No tienes cuenta? Regístrate';
  }

  mostrarMensajeAuth('');

  const campoInicial = document.getElementById(
    mostrarRegistro ? 'registerUsername' : 'loginIdentifier'
  );

  if (campoInicial) {
    campoInicial.focus();
  }
}

// Presenta mensajes dentro del formulario de autenticación.
function mostrarMensajeAuth(mensaje) {
  const elemento = document.getElementById('authMessage');

  if (elemento) {
    elemento.textContent = mensaje;
  }
}

// Registra una nueva cuenta.
async function registrarUsuario(event) {
  event.preventDefault();

  const username = document.getElementById('registerUsername').value.trim();
  const email = document.getElementById('registerEmail').value.trim();
  const password = document.getElementById('registerPassword').value;

  const boton = document.querySelector('#registerForm button[type="submit"]');

  if (boton) {
    boton.disabled = true;
    boton.textContent = 'Creando cuenta...';
  }

  try {
    const respuesta = await realizarPeticion(
      `${API_URL}/usuarios/registro`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json'
        },
        body: JSON.stringify({
          username,
          email,
          password
        })
      }
    );

    if (!respuesta.ok) {
      throw new Error(await obtenerMensajeError(respuesta));
    }

    const loginIdentifier = document.getElementById('loginIdentifier');

    if (loginIdentifier) {
      loginIdentifier.value = username;
    }

    document.getElementById('registerForm').reset();

    alternarFormularioAuth();

    mostrarMensajeAuth(
      'Cuenta creada correctamente. Ahora puedes iniciar sesión.'
    );

  } catch (error) {
    mostrarMensajeAuth(
      error.message || 'No se pudo crear la cuenta'
    );
  } finally {
    if (boton) {
      boton.disabled = false;
      boton.textContent = 'Crear cuenta';
    }
  }
}

// Inicia sesión y guarda el JWT recibido.
async function iniciarSesion(event) {
  event.preventDefault();

  const identifier = document.getElementById('loginIdentifier').value.trim();
  const password = document.getElementById('loginPassword').value;

  const boton = document.querySelector('#loginForm button[type="submit"]');

  if (boton) {
    boton.disabled = true;
    boton.textContent = 'Iniciando sesión...';
  }

  try {
    const respuesta = await realizarPeticion(
      `${API_URL}/usuarios/login`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json'
        },
        body: JSON.stringify({
          identifier,
          password
        })
      }
    );

    if (!respuesta.ok) {
      throw new Error(await obtenerMensajeError(respuesta));
    }

    const data = await respuesta.json();

    if (
      !data.access_token ||
      data.token_type !== 'Bearer' ||
      !data.user
    ) {
      throw new Error('El servidor devolvió una respuesta de sesión inválida');
    }

    guardarSesion(data.access_token, data.user);

    document.getElementById('loginForm').reset();

    mostrarMensajeAuth('');

    alertaPersistente = false;
    errorConexionMostrado = false;

    mostrarAlerta(`Bienvenido, ${data.user.username}`);

    renderizarTablero();

  } catch (error) {
    mostrarMensajeAuth(
      error.message || 'No se pudo iniciar sesión'
    );
  } finally {
    if (boton) {
      boton.disabled = false;
      boton.textContent = 'Iniciar sesión';
    }
  }
}

// Cierra la sesión actual.
function cerrarSesion() {
  limpiarSesionLocal();

  piezaSeleccionada = null;
  alertaPersistente = false;

  actualizarInterfazAutenticacion();
  renderizarTablero();

  mostrarAlerta('Sesión cerrada correctamente');
}

// =========================================
// SINCRONIZACIÓN DEL JUEGO
// =========================================

// Mantiene sincronizado el tablero con el estado del servidor.
function iniciarPolling() {
  if (intervalId !== null) {
    clearInterval(intervalId);
  }

  intervalId = setInterval(() => {
    obtenerEstadoJuego();
  }, POLLING_INTERVAL_MS);
}

// Actualiza el indicador de la instancia que atendió la solicitud.
function actualizarTrackerInstancia(respuesta) {
  const instanceId =
    respuesta.headers.get('X-Instance-Id') || 'No disponible';

  const trackerEl = document.getElementById('instanceTracker');

  if (trackerEl) {
    trackerEl.textContent = `Procesado por backend: ${instanceId}`;
  }
}

// Convierte valores de booleano recibidos desde el servidor.
function normalizarBooleano(valor) {
  if (valor === true || valor === false) {
    return valor;
  }

  if (valor === 'true' || valor === 1) {
    return true;
  }

  if (valor === 'false' || valor === 0) {
    return false;
  }

  throw new Error('El servidor devolvió un estado de pieza inválido');
}

// Comprueba y normaliza los datos recibidos del backend.
function normalizarEstadoJuego(data) {
  if (
    !data ||
    typeof data !== 'object' ||
    !Array.isArray(data.piezas)
  ) {
    throw new Error('El servidor devolvió un estado de partida inválido');
  }

  const idJuego = Number(data.id);

  if (!Number.isInteger(idJuego) || idJuego < 1) {
    throw new Error('El servidor devolvió un identificador de partida inválido');
  }

  const piezas = data.piezas.map((pieza) => {
    if (!pieza || typeof pieza !== 'object') {
      throw new Error('El servidor devolvió una pieza inválida');
    }

    const id = Number(pieza.id);

    const posX = String(pieza.pos_x || '').toUpperCase();
    const posY = Number(pieza.pos_y);

    const jugador = String(pieza.jugador || '').toUpperCase();
    const tipo = String(pieza.tipo || '').toUpperCase();

    if (
      !Number.isInteger(id) ||
      id < 1 ||
      !COLUMNAS.includes(posX) ||
      !Number.isInteger(posY) ||
      posY < 1 ||
      posY > 5 ||
      !JUGADORES_VALIDOS.includes(jugador) ||
      !TIPOS_VALIDOS.includes(tipo)
    ) {
      throw new Error('El servidor devolvió una pieza con datos inválidos');
    }

    const valorVivo =
      pieza.esta_viva !== undefined
        ? pieza.esta_viva
        : pieza.estaViva;

    if (valorVivo === undefined) {
      throw new Error('El servidor no indicó si la pieza está viva');
    }

    return {
      id,
      jugador,
      tipo,
      pos_x: posX,
      pos_y: posY,
      esta_viva: normalizarBooleano(valorVivo)
    };
  });

  const turnoActual = String(
    data.turno_actual || data.turnoActual || 'WHITE'
  ).toUpperCase();

  const estado = String(
    data.estado || 'IN_PROGRESS'
  ).toUpperCase();

  if (!JUGADORES_VALIDOS.includes(turnoActual)) {
    throw new Error('El servidor devolvió un turno inválido');
  }

  if (!ESTADOS_VALIDOS.includes(estado)) {
    throw new Error('El servidor devolvió un estado de partida desconocido');
  }

  return {
    id: idJuego,
    turnoActual,
    estado,
    piezas
  };
}

// Aplica un estado válido y actualiza la representación visual.
function aplicarEstadoJuego(data) {
  estadoJuego = normalizarEstadoJuego(data);

  if (piezaSeleccionada) {
    piezaSeleccionada =
      estadoJuego.piezas.find(
        pieza =>
          pieza.id === piezaSeleccionada.id &&
          pieza.esta_viva
      ) || null;
  }

  actualizarBannerEstado();
  renderizarTablero();
}

// Realiza peticiones HTTP con un tiempo máximo de espera.
async function realizarPeticion(url, opciones = {}) {
  const controlador = new AbortController();

  const timeoutId = setTimeout(() => {
    controlador.abort();
  }, REQUEST_TIMEOUT_MS);

  try {
    return await fetch(url, {
      ...opciones,
      signal: controlador.signal,
      cache: 'no-store'
    });

  } catch (error) {
    if (error.name === 'AbortError') {
      throw new Error('El servidor tardó demasiado en responder');
    }

    throw error;

  } finally {
    clearTimeout(timeoutId);
  }
}

// Obtiene el estado sin perder actualizaciones pendientes.
async function obtenerEstadoJuego() {
  if (accionEnCurso) {
    consultaPendiente = true;
    return;
  }

  if (consultaEnCurso) {
    consultaPendiente = true;
    return;
  }

  consultaEnCurso = true;
  consultaPendiente = false;

  const versionConsulta = versionEstado;

  try {
    const respuesta = await realizarPeticion(`${API_URL}/juego`, {
      method: 'GET',
      headers: {
        Accept: 'application/json'
      }
    });

    actualizarTrackerInstancia(respuesta);

    if (!respuesta.ok) {
      throw new Error(await obtenerMensajeError(respuesta));
    }

    const data = await respuesta.json();

    if (
      versionConsulta !== versionEstado ||
      accionEnCurso
    ) {
      return;
    }

    alertaPersistente = false;
    errorConexionMostrado = false;

    aplicarEstadoJuego(data);

  } catch (error) {
    console.error('Error consultando el estado:', error);

    if (
      versionConsulta === versionEstado &&
      !accionEnCurso &&
      !errorConexionMostrado
    ) {
      mostrarAlerta(
        error.message || 'Error conectando al cluster distribuido'
      );

      errorConexionMostrado = true;
    }

  } finally {
    consultaEnCurso = false;

    if (consultaPendiente && !accionEnCurso) {
      consultaPendiente = false;
      obtenerEstadoJuego();
    }
  }
}

// Extrae el mensaje de error del formato JSON utilizado por el Gateway.
async function obtenerMensajeError(respuesta) {
  const texto = await respuesta.text();

  if (!texto) {
    return `Error HTTP ${respuesta.status}`;
  }

  try {
    const data = JSON.parse(texto);

    return data.error || `Error HTTP ${respuesta.status}`;

  } catch {
    return `Error HTTP ${respuesta.status}: ${texto}`;
  }
}

// =========================================
// OPERACIONES AUTENTICADAS
// =========================================

// Envía el JWT en las operaciones protegidas.
function obtenerHeadersAutenticados(headersOriginales = {}) {
  const headers = new Headers(headersOriginales);

  if (accessToken) {
    headers.set('Authorization', `Bearer ${accessToken}`);
  }

  return headers;
}

// Envía una operación y devuelve el estado actualizado.
async function ejecutarAccion(url, opciones = {}) {
  if (accionEnCurso) {
    return;
  }

  if (!accessToken || !usuarioActual) {
    mostrarAlerta('Debes iniciar sesión para realizar esta operación');
    return;
  }

  accionEnCurso = true;
  consultaPendiente = false;

  versionEstado++;

  piezaSeleccionada = null;
  renderizarTablero();

  try {
    const respuesta = await realizarPeticion(url, {
      ...opciones,
      headers: obtenerHeadersAutenticados(opciones.headers)
    });

    actualizarTrackerInstancia(respuesta);

    if (respuesta.status === 401) {
      cerrarSesion();
      throw new Error('Tu sesión expiró o dejó de ser válida. Inicia sesión nuevamente.');
    }

    if (!respuesta.ok) {
      throw new Error(await obtenerMensajeError(respuesta));
    }

    const data = await respuesta.json();

    aplicarEstadoJuego(data);

    errorConexionMostrado = false;
    alertaPersistente = false;

    return data;

  } catch (error) {
    console.error('Error ejecutando operación:', error);

    alertaPersistente = true;

    if (
      error.message === 'El servidor tardó demasiado en responder'
    ) {
      mostrarAlerta(error.message);
    } else if (error instanceof TypeError) {
      mostrarAlerta(
        'No se pudo establecer comunicación con el servidor'
      );
    } else {
      mostrarAlerta(
        error.message || 'Error al procesar la operación'
      );
    }

    throw error;

  } finally {
    accionEnCurso = false;

    versionEstado++;

    obtenerEstadoJuego();
  }
}

// Solicita un movimiento al Game Service.
async function realizarMovimiento(idPieza, destinoX, destinoY) {
  if (accionEnCurso) {
    return;
  }

  try {
    await ejecutarAccion(`${API_URL}/mover`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json'
      },
      body: JSON.stringify({
        id_pieza: idPieza,
        destino_x: destinoX,
        destino_y: destinoY
      })
    });

  } catch (error) {
    // El mensaje ya fue presentado por ejecutarAccion.
  }
}

// Solicita el reinicio de la partida.
async function reiniciarPartida() {
  if (accionEnCurso) {
    return;
  }

  if (!accessToken) {
    mostrarAlerta('Inicia sesión para reiniciar la partida');
    return;
  }

  try {
    await ejecutarAccion(`${API_URL}/reiniciar`, {
      method: 'POST',
      headers: {
        Accept: 'application/json'
      }
    });

    mostrarAlerta('Juego reiniciado');

  } catch (error) {
    // El mensaje ya fue presentado por ejecutarAccion.
  }
}

// =========================================
// TABLERO
// =========================================

// Comprueba si una pieza está viva.
function piezaEstaViva(pieza) {
  return pieza.esta_viva === true;
}

// Busca la pieza viva que ocupa una casilla.
function obtenerPiezaEnCasilla(posX, posY) {
  return estadoJuego.piezas.find(
    pieza =>
      pieza.pos_x === posX &&
      pieza.pos_y === posY &&
      piezaEstaViva(pieza)
  );
}

// Dibuja las 25 casillas y las piezas existentes.
function renderizarTablero() {
  const contenedorTablero = document.getElementById('chessBoard');

  if (!contenedorTablero) {
    return;
  }

  contenedorTablero.innerHTML = '';

  for (let fila = 5; fila >= 1; fila--) {
    COLUMNAS.forEach((columna, colIndex) => {
      const casilla = document.createElement('div');

      casilla.classList.add('tile');

      const esClara = (colIndex + fila) % 2 !== 0;

      casilla.classList.add(
        esClara ? 'light' : 'dark'
      );

      casilla.dataset.posX = columna;
      casilla.dataset.posY = fila;

      const pieza = obtenerPiezaEnCasilla(columna, fila);

      if (pieza) {
        const elementoPieza = document.createElement('div');

        elementoPieza.classList.add('piece');

        const colorClase = pieza.jugador.toLowerCase();
        const tipoClase = pieza.tipo.toLowerCase();

        elementoPieza.classList.add(
          `${colorClase}-${tipoClase}`
        );

        casilla.appendChild(elementoPieza);
      }

      if (
        piezaSeleccionada &&
        piezaSeleccionada.pos_x === columna &&
        piezaSeleccionada.pos_y === fila
      ) {
        casilla.classList.add('selected');
      }

      if (accionEnCurso) {
        casilla.classList.add('disabled');
      }

      casilla.addEventListener('click', () => {
        manejarClickCasilla(columna, fila);
      });

      contenedorTablero.appendChild(casilla);
    });
  }
}

// Gestiona la selección de piezas y el destino de los movimientos.
function manejarClickCasilla(posX, posY) {
  if (accionEnCurso) {
    return;
  }

  if (!accessToken) {
    mostrarAlerta('Inicia sesión para mover piezas');
    return;
  }

  if (estadoJuego.estado !== 'IN_PROGRESS') {
    mostrarAlerta('La partida ha finalizado');
    return;
  }

  const piezaEnCasilla = obtenerPiezaEnCasilla(posX, posY);

  if (!piezaSeleccionada) {
    if (piezaEnCasilla) {
      if (piezaEnCasilla.jugador === estadoJuego.turnoActual) {
        piezaSeleccionada = piezaEnCasilla;
        renderizarTablero();
      } else {
        mostrarAlerta(
          `Es el turno de las ${
            estadoJuego.turnoActual === 'WHITE' ? 'Blancas' : 'Negras'
          }`
        );
      }
    }

    return;
  }

  if (piezaEnCasilla && piezaEnCasilla.id === piezaSeleccionada.id) {
    piezaSeleccionada = null;
    renderizarTablero();
    actualizarBannerEstado();
    return;
  }

  if (
    piezaEnCasilla &&
    piezaEnCasilla.jugador === piezaSeleccionada.jugador
  ) {
    piezaSeleccionada = piezaEnCasilla;
    renderizarTablero();
    return;
  }

  realizarMovimiento(
    piezaSeleccionada.id,
    posX,
    posY
  );
}

// Actualiza el mensaje que indica el estado de la partida.
function actualizarBannerEstado() {
  const elementoAlerta = document.getElementById('gameAlert');

  if (!elementoAlerta || alertaPersistente) {
    return;
  }

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

    case 'WAITING':
      elementoAlerta.textContent = 'Esperando el inicio de la partida';
      break;

    default:
      elementoAlerta.textContent =
        `Turno de las ${
          estadoJuego.turnoActual === 'WHITE' ? 'Blancas' : 'Negras'
        }`;
  }
}

// Presenta mensajes de interacción o errores en el área de alertas.
function mostrarAlerta(mensaje) {
  const elementoAlerta = document.getElementById('gameAlert');

  if (elementoAlerta) {
    elementoAlerta.textContent = mensaje;
  }
}
