\# Mini Chess



Mini Chess es una aplicación de ajedrez basada en una arquitectura de microservicios. El proyecto integra servicios backend independientes, comunicación mediante gRPC, persistencia en PostgreSQL y mensajería asíncrona mediante Apache Kafka.



El objetivo es ofrecer una plataforma modular que permita gestionar usuarios y partidas de ajedrez, procesar movimientos y comunicar eventos entre servicios sin bloquear las operaciones principales.



\---



\## 1. Arquitectura del proyecto



El sistema está organizado en diferentes componentes:



\- \*\*Frontend:\*\* interfaz de usuario para interactuar con la aplicación.

\- \*\*Users Service:\*\* registro, autenticación y gestión de usuarios.

\- \*\*Game Service:\*\* administración de partidas, movimientos y reinicios.

\- \*\*Events Consumer:\*\* microservicio independiente encargado de consumir y procesar eventos de Kafka.

\- \*\*PostgreSQL:\*\* almacenamiento persistente de información.

\- \*\*Apache Kafka:\*\* sistema de mensajería asíncrona entre los servicios backend.



\### Diagrama de arquitectura



```mermaid

flowchart TD

&#x20;   A\[Frontend Mini Chess]



&#x20;   A --> B\[Users Service]

&#x20;   A --> C\[Game Service]



&#x20;   B --> D\[(PostgreSQL)]

&#x20;   C --> D



&#x20;   B --> E\[Kafka]

&#x20;   C --> E



&#x20;   E --> F\[users.events]

&#x20;   E --> G\[games.events]



&#x20;   F --> H\[Events Consumer]

&#x20;   G --> H



&#x20;   H --> I\[Event Handlers]

```



\### Comunicación entre servicios



Los servicios backend utilizan gRPC para la comunicación interna correspondiente.



Apache Kafka se utiliza para distribuir eventos de manera asíncrona, permitiendo que las operaciones principales continúen sin esperar a que los consumidores terminen de procesarlos.



\---



\## 2. Tecnologías utilizadas



| Tecnología | Función |

|---|---|

| Go | Desarrollo de servicios backend |

| gRPC | Comunicación entre servicios |

| Protocol Buffers | Definición de contratos gRPC |

| PostgreSQL | Base de datos relacional |

| Apache Kafka 4.1.0 | Mensajería asíncrona |

| kafka-go | Integración de Kafka con Go |

| Docker | Contenerización |

| Docker Compose | Orquestación de servicios |

| JWT | Autenticación mediante tokens |

| bcrypt | Protección de contraseñas |



\---



\## 3. Estructura del proyecto



```text

New Mini Chess/

│

├── docker-compose.yml

├── README.md

├── .env

├── backup\_minichess.dump

│

├── proto/

│   ├── user.proto

│   ├── game.proto

│   └── pb/

│       ├── user.pb.go

│       ├── user\_grpc.pb.go

│       ├── game.pb.go

│       └── game\_grpc.pb.go

│

├── services/

│   │

│   ├── users-service/

│   │   ├── main.go

│   │   ├── kafka.go

│   │   └── go.mod

│   │

│   ├── game-service/

│   │   ├── main.go

│   │   ├── kafka.go

│   │   └── go.mod

│   │

│   └── events-consumer/

│       ├── main.go

│       ├── consumer.go

│       ├── handlers.go

│       ├── Dockerfile

│       ├── go.mod

│       └── go.sum

│

└── db/

&#x20;   ├── 01\_schema.sql

&#x20;   └── 02\_init\_data.sql

```



\*Nota: la estructura muestra los archivos principales de cada componente; pueden existir otros archivos adicionales.\*



\---



\## 4. Servicios backend



\### 4.1 Users Service



Responsable de la gestión de usuarios.



Sus principales funcionalidades incluyen:



\- Registro de usuarios.

\- Inicio de sesión.

\- Consulta de información de usuarios.

\- Validación de datos.

\- Protección de contraseñas mediante bcrypt.

\- Autenticación mediante JWT.

\- Persistencia en PostgreSQL.



Después de registrar correctamente un usuario, el servicio publica un evento en Kafka.



\### 4.2 Game Service



Responsable de la administración de partidas de ajedrez.



Sus funcionalidades incluyen:



\- Obtener el estado de una partida.

\- Validar movimientos.

\- Ejecutar movimientos legales.

\- Gestionar capturas.

\- Gestionar promoción de peones.

\- Reiniciar partidas.

\- Mantener la consistencia de las operaciones mediante transacciones y bloqueos en PostgreSQL.



El servicio publica eventos después de completar exitosamente las operaciones correspondientes en la base de datos.



\### 4.3 Events Consumer



Es un microservicio independiente desarrollado en Go.



Su responsabilidad es recibir, interpretar y procesar los eventos publicados por los productores de Kafka.



Implementa:



\- Un consumidor para `users.events`.

\- Un consumidor para `games.events`.

\- Grupos de consumidores independientes.

\- Validación de mensajes JSON.

\- Reintentos de procesamiento.

\- Confirmación de offsets después del procesamiento exitoso.

\- Cierre controlado mediante contextos y señales del sistema operativo.



\---



\## 5. Implementación de Apache Kafka



\### 5.1 Objetivo



Se implementó una arquitectura de mensajería asíncrona utilizando Apache Kafka para desacoplar la publicación y el procesamiento de eventos.



Los servicios principales no necesitan esperar a que los consumidores procesen los mensajes para completar sus operaciones.



La integración mantiene los flujos existentes de HTTP y gRPC.



\### 5.2 Tópicos implementados



Se configuraron dos tópicos:



| Tópico | Servicio productor | Eventos |

|---|---|---|

| `users.events` | Users Service | `user.registered` |

| `games.events` | Game Service | `game.move\_completed`, `game.reset` |



El tópico `matchmaking.events` queda pendiente de implementación, ya que todavía no existe un flujo operativo de matchmaking.



\### 5.3 Eventos publicados



\*\*Evento de registro de usuario\*\*



```json

{

&#x20; "event\_id": "unique-event-id",

&#x20; "event\_type": "user.registered",

&#x20; "user\_id": 6,

&#x20; "username": "example",

&#x20; "created\_at": "2026-10-30T10:40:00Z"

}

```



\*\*Evento de movimiento\*\*



```json

{

&#x20; "event\_id": "unique-event-id",

&#x20; "event\_type": "game.move\_completed",

&#x20; "game\_id": 1,

&#x20; "instance\_id": "game-service-node-01",

&#x20; "details": "piece\_id=68,from=C2,to=C3,promotion=",

&#x20; "created\_at": "2026-10-30T10:40:00Z"

}

```



\*\*Evento de reinicio de partida\*\*



```json

{

&#x20; "event\_id": "unique-event-id",

&#x20; "event\_type": "game.reset",

&#x20; "game\_id": 1,

&#x20; "instance\_id": "game-service-node-02",

&#x20; "details": "board\_restored\_to\_initial\_position",

&#x20; "created\_at": "2026-10-30T10:40:00Z"

}

```



Los identificadores y las marcas de tiempo son generados por los servicios productores.



\---



\## 6. Productores Kafka



Los productores están implementados de manera independiente en cada servicio backend.



Archivos principales:



\- `services/users-service/kafka.go`

\- `services/game-service/kafka.go`



\### Características



\- Publicación asíncrona mediante una cola interna en memoria.

\- Capacidad de cola limitada a 100 eventos.

\- Procesamiento de publicaciones en una goroutine independiente.

\- Hasta tres intentos de publicación.

\- Registro de errores mediante logs.

\- Comportamiento no bloqueante cuando la cola está llena.

\- Publicación de eventos de partidas únicamente después de confirmar la transacción en PostgreSQL.



La configuración permite que los servicios principales mantengan su funcionamiento incluso cuando Kafka presenta problemas temporales.



\*\*Importante:\*\* la cola interna está en memoria. Los eventos pueden perderse si el proceso se detiene antes de publicarlos o si la cola está llena. Actualmente no se utiliza un patrón Transactional Outbox.



\---



\## 7. Consumidores Kafka



El microservicio `events-consumer` utiliza la biblioteca `github.com/segmentio/kafka-go`.



Se implementaron dos grupos independientes:



| Tópico | Grupo |

|---|---|

| `users.events` | `users-events-group` |

| `games.events` | `games-events-group` |



\### 7.1 Procesamiento de mensajes



El flujo de procesamiento es:



1\. El consumidor recibe un mensaje de Kafka.

2\. El mensaje se entrega al handler correspondiente.

3\. Se valida y decodifica el contenido JSON.

4\. Se verifica el tipo de evento y sus campos obligatorios.

5\. Se procesa el evento.

6\. Se confirma el offset únicamente después del procesamiento exitoso.



\### 7.2 Reintentos



Los handlers cuentan con hasta tres intentos de procesamiento.



Entre los intentos se aplica una espera progresiva.



Si los tres intentos fallan:



\- El mensaje no se confirma.

\- El consumidor registra el error.

\- La ejecución del consumidor termina.

\- Docker puede reiniciar el contenedor mediante su política de reinicio.



Actualmente no se cuenta con una Dead Letter Queue (DLQ).



\### 7.3 Handlers implementados



Archivo: `services/events-consumer/handlers.go`



\*\*handleUserEvent\*\*



\- Decodifica eventos de usuario.

\- Reconoce `user.registered`.

\- Valida el identificador y nombre del usuario.

\- Registra el procesamiento exitoso.



\*\*handleGameEvent\*\*



\- Decodifica eventos de partida.

\- Valida el identificador de partida.

\- Reconoce `game.move\_completed`.

\- Reconoce `game.reset`.

\- Registra los detalles del evento procesado.



Los handlers actuales validan y registran los eventos. No realizan todavía operaciones adicionales sobre otros servicios o bases de datos.



\---



\## 8. Configuración de Docker Compose



Kafka está integrado en el entorno Docker Compose.



Configuración general:



| Componente | Responsabilidad |

|---|---|

| `db` | PostgreSQL |

| `kafka` | Broker de mensajería |

| `users-service` | Gestión de usuarios y publicación de eventos |

| `game-service-1` | Gestión de partidas y publicación de eventos |

| `game-service-2` | Segunda réplica del servicio de partidas |

| `events-consumer` | Consumo y procesamiento de eventos |



Los servicios productores utilizan:



```yaml

KAFKA\_BROKERS: kafka:9092

```



El consumidor también utiliza:



```yaml

KAFKA\_BROKERS: kafka:9092

```



El consumidor depende de que Kafka esté saludable antes de iniciar.



\### Tópicos



Los tópicos configurados son:



\- `users.events`

\- `games.events`



Cada tópico utiliza una partición y un factor de replicación de uno.



\---



\## 9. Instalación y ejecución



\### Requisitos



\- Docker Desktop.

\- Docker Compose.

\- Go compatible con los módulos del proyecto.

\- Git.



\### Configuración



El proyecto utiliza variables de entorno definidas en `.env`.



Es necesario configurar las credenciales de PostgreSQL y los secretos de autenticación antes de iniciar los servicios.



No se deben subir credenciales, contraseñas ni tokens al repositorio.



\### Validar Docker Compose



```cmd

docker compose config -q

```



\### Consultar servicios



```cmd

docker compose config --services

```



\### Construir e iniciar el entorno



```cmd

docker compose up -d --build

```



\### Consultar el estado de los contenedores



```cmd

docker compose ps

```



\### Consultar los logs



```cmd

docker compose logs -f

```



\### Consultar los logs del consumidor



```cmd

docker compose logs -f events-consumer

```



\### Reiniciar únicamente el consumidor



```cmd

docker compose restart events-consumer

```



\---



\## 10. Pruebas de Kafka



Se realizaron pruebas de extremo a extremo utilizando los servicios reales del proyecto.



\### 10.1 Verificar los tópicos



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list

```



Resultado esperado:



```text

\_\_consumer\_offsets

games.events

users.events

```



\### 10.2 Verificar los grupos de consumidores



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list

```



Grupos esperados:



```text

games-events-group

users-events-group

```



\### 10.3 Verificar offsets de partidas



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group games-events-group

```



\### 10.4 Verificar offsets de usuarios



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group users-events-group

```



\### 10.5 Consumir mensajes manualmente



Para consultar mensajes de partidas:



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic games.events --from-beginning

```



Para consultar mensajes de usuarios:



```cmd

docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic users.events --from-beginning

```



\### 10.6 Pruebas realizadas



Se verificaron los siguientes escenarios:



| Prueba | Resultado |

|---|---|

| Publicación de registro de usuario | Exitosa |

| Consumo de `user.registered` | Exitosa |

| Confirmación del offset de usuario | Exitosa |

| Publicación de movimiento | Exitosa |

| Consumo de `game.move\_completed` | Exitosa |

| Publicación de reinicio de partida | Exitosa |

| Consumo de eventos de partidas | Exitosa |

| Confirmación de offsets de partidas | Exitosa |

| Reconexión del consumidor después de reinicio | Exitosa |



\### Evidencia de procesamiento



Durante las pruebas se obtuvieron registros equivalentes a los siguientes:



```text

Mensaje recibido: tópico=users.events, partición=0, offset=0



Usuario registrado procesado:

ID=6, username=consumertest



Mensaje confirmado:

tópico=users.events, offset=0

```



Para partidas:



```text

Mensaje recibido:

tópico=games.events, partición=0, offset=0



Movimiento procesado:

partida=1



Mensaje confirmado:

tópico=games.events, offset=0

```



También se verificó el procesamiento de un segundo movimiento y su confirmación.



\---



\## 11. Pruebas de compilación



Los módulos backend fueron compilados y verificados mediante Go.



Para Game Service:



```cmd

cd services\\game-service

go test ./...

```



Para Events Consumer:



```cmd

cd services\\events-consumer

go test ./...

```



Para Users Service:



```cmd

cd services\\users-service

go test ./...

```



La compilación y ejecución de las pruebas disponibles se realizaron correctamente. La ausencia de archivos de pruebas en un módulo significa que Go confirma su compilación, pero no implica que existan pruebas unitarias automatizadas.



\---



\## 12. Decisiones técnicas y limitaciones



\### Mensajería asíncrona



La publicación de eventos se realiza mediante colas internas en memoria para reducir el impacto de Kafka sobre las operaciones principales.



\### Confirmación de mensajes



Los consumidores confirman los offsets después de procesar correctamente los mensajes.



Esto proporciona una semántica de procesamiento \*at-least-once\*.



Por tanto, un mensaje podría procesarse más de una vez si ocurre una interrupción después del procesamiento, pero antes de confirmar el offset. Los handlers futuros que produzcan efectos secundarios deberán considerar la idempotencia.



\### Disponibilidad



La configuración actual de Kafka utiliza:



\- Un solo broker.

\- Una partición por tópico.

\- Factor de replicación uno.



Es una configuración apropiada para desarrollo y demostración, pero no ofrece alta disponibilidad.



\### Persistencia



Actualmente Kafka no tiene un volumen persistente dedicado configurado en Docker Compose.



Por ello, recrear el contenedor o perder sus datos puede provocar la pérdida de mensajes y offsets almacenados.



La base de datos PostgreSQL utiliza un volumen independiente que debe conservarse.



\### Entrega de eventos



No se utiliza todavía Transactional Outbox ni una estrategia de entrega exactamente una vez.



La publicación de eventos y las transacciones de PostgreSQL no forman una única transacción distribuida.



\### Matchmaking



El sistema todavía no cuenta con un servicio operativo de matchmaking. Por ello, no se ha implementado un tópico específico para esta funcionalidad.



\---



\## 13. Seguridad



\- Las credenciales deben almacenarse en variables de entorno.

\- El archivo `.env` no debe incluirse en Git.

\- No deben publicarse tokens JWT ni contraseñas.

\- Las contraseñas de usuarios se protegen mediante bcrypt.

\- La autenticación utiliza JWT.

\- Las operaciones de base de datos deben mantener sus controles transaccionales.



\---



\## 14. Mejoras futuras



Entre las posibles mejoras se encuentran:



1\. Implementar Transactional Outbox para aumentar la confiabilidad de publicación.

2\. Incorporar una Dead Letter Queue para mensajes que no puedan procesarse.

3\. Implementar idempotencia en los consumidores.

4\. Configurar persistencia de Kafka mediante un volumen dedicado.

5\. Incorporar más brokers y replicación para alta disponibilidad.

6\. Crear un servicio de matchmaking y su tópico correspondiente.

7\. Incorporar métricas y monitoreo de Kafka.

8\. Desarrollar pruebas unitarias y de integración automatizadas.

9\. Incorporar procesamiento de eventos con efectos adicionales en otros servicios.



\---



\## 15. Estado de la entrega Kafka



La integración de Apache Kafka incluye:



\- Productores asíncronos en Users Service y Game Service.

\- Tópicos independientes por dominio.

\- Colas internas para publicación.

\- Consumidores independientes por tópico.

\- Validación y procesamiento de eventos.

\- Reintentos ante fallos.

\- Confirmación de offsets después del procesamiento.

\- Integración con Docker Compose.

\- Pruebas de extremo a extremo.



La implementación fue verificada mediante registros reales de usuarios, movimientos y reinicios de partidas.



\---



\## 16. Licencia y contribuciones



Este proyecto forma parte del desarrollo de Mini Chess.



Las contribuciones, correcciones y mejoras deben mantener la compatibilidad de los servicios existentes y preservar la integridad de los datos almacenados.



Para realizar cambios en la arquitectura, se recomienda documentar las modificaciones y verificar los servicios mediante pruebas antes de integrarlas a la rama principal.



