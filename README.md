# Tuya Beacon Mesh P2 Controller — CLI та MQTT-сервер

Для збірки CLI та сервера під Linux amd64 і arm64 запустіть `./build.sh`. Скрипт перевіряє форматування, модулі, запускає `go vet` і тести з детектором гонок, а потім записує чотири бінарники в `build/`.

Зберіть обидві команди з кореня модуля (Go 1.23+).

```sh
go build -o tuya-beacon-mesh-p2-controller ./cmd/tuya-beacon-mesh-p2-controller
go build -o tuya-beacon-mesh-p2-controller-server ./cmd/tuya-beacon-mesh-p2-controller-server
./tuya-beacon-mesh-p2-controller-server
```

Для наявних MQTT-автоматизацій зі старим типовим префіксом задайте `"topic_prefix": "tuya-p2"` у секції `mqtt` і збережіть свій `client_id`.

CLI та сервер за замовчуванням читають `config.json` із поточного робочого каталогу. Інший шлях можна задати через `TUYA_BEACON_MESH_P2_CONTROLLER_CONFIG` або прапорець `-config`, який має найвищий пріоритет.

Сервер читає налаштування пристрою та перевіряє state перед підключенням до MQTT. Після втрати зв’язку він повторює спробу через 5 секунд. Для зупинки використовуйте Ctrl+C або SIGTERM. У цій версії сервер запускає MQTT і потребує `mqtt.enabled=true`.

Для переходу з `tuya-beacon-mesh-p2-controller mqtt` зупиніть запущений міст і запустіть `tuya-beacon-mesh-p2-controller-server` із тим самим конфігом і state. Обидва способи використовують той самий MQTT client ID, топіки та Home Assistant Discovery. Разові CLI-команди можна запускати поруч із сервером.

```sh
./tuya-beacon-mesh-p2-controller -config ./config.json state show
./tuya-beacon-mesh-p2-controller -config ./config.json -dry-run on
```

CLI та сервер зберігають state у `./state/state-<device-id>.json` відносно робочого каталогу запуску. Перед оновленням зупиніть попередню версію. При першому запуску застосунок імпортує наявний state з робочого каталогу або каталогу бінарника, обираючи копію з найбільшим `seq`. Якщо бінарники запускаються з різних робочих каталогів, задайте один абсолютний шлях у `controller.state` або передавайте однаковий `-state` обом командам.

### Структура Go-модуля

У `cmd/` розташовані точки входу. `internal/cli` обробляє командний рядок, `internal/app` збирає контролер і запускає сервіси. MQTT та CLI викликають `internal/controller` для команд лампи. Майбутній web-сервіс може використовувати його методи `Execute` та `Snapshot`.

Протокол і сцени містяться в `internal/p2`, транспорт Bluetooth — у `internal/bluez`, конфігурація й файловий стан — у `internal/config` та `internal/state`. MQTT, моніторинг, читання записів і отримання ключів мають окремі пакети `mqtt`, `monitor`, `capture` та `provision`.

# v9.3.6 — Main light entity label

Home Assistant device remains `Smart bulb (<device-id>)`, but its primary light entity now has a distinct label `Light`. Set `mqtt.light_name` in `config.json` to change the entity's label. Previous `name: null` has been replaced; the original unique_id, object_id, discovery topic and state/command topics are unchanged. Home Assistant manual name overrides take priority.

# tuya-beacon-mesh-p2-controller v9.3.5 — Zigbee2MQTT-style Tuya model and normal HA toggle

In `config.json`, under `device`, add `"model": "Smart Bulb 2 (Tuya Beacon Mesh P2)"`. The program combines it with `product_id` for Home Assistant MQTT Discovery.

- **Device**: `Smart bulb (test-lamp)`
- **Description / model**: `Smart Bulb 2 (Tuya Beacon Mesh P2) (wbl39qpt)` **by Tuya**
- **Connected via**: `tuya-beacon-mesh-p2-controller Bridge`
- **Firmware**: `1.2` from `device.firmware`
- **Hardware**: `wbl39qpt` from `device.product_id`

Existing installations of the known device that have no `device.model` also show this model via a device-specific fallback. For other devices the model must be configured. A model already ending in `(product_id)` will not get the ID appended twice.

MQTT light Discovery now advertises `optimistic: false` and uses its existing `ha/state` topic, so Home Assistant should show **one ON/OFF toggle**. This does **not** imply BLE acknowledgement: state comes from the local command/monitor, as before. You may need to reload MQTT or the device if the old assumed-state display is cached. Unique IDs and topics stay stable.

See `CHANGES-v9.3.5.md` for the changes; older release notes below are historical.

---

# tuya-beacon-mesh-p2-controller v9.3.4 — Zigbee2MQTT-style MQTT Bridge + Tuya Lamp

У Home Assistant тепер відображаються два **логічні пристрої**, як у Zigbee2MQTT:

- **`tuya-beacon-mesh-p2-controller Bridge`** — виробник `AzaOne`, модель `Bridge`, прошивка `9.3.4`. Діагностика: `Bridge state`, `Version`, `Adapter`, `Transport`. Міст є MQTT-процесом, а не доказом доступності фізичної лампи.
- **`Smart bulb (test-lamp)`** — виробник `Tuya`, firmware `1.2`, hardware `wbl39qpt`, **Connected via tuya-beacon-mesh-p2-controller Bridge**. Рівно дві сутності: лампа `light` (живлення, RGB, температура, яскравість, 28 ефектів) і `number` (`Effect speed`).

Це **та сама MQTT-інтеграція**, що й раніше. Збережено старі discovery topic і unique_id для світла, швидкості та колишнього `Connection` датчика: після запуску старі сутності оновляться, а не створять нові. Для світла `name: null`, як у Zigbee2MQTT: основна сутність носить ім'я пристрою без зайвого дублювання.

### Структура MQTT

```text
tuya-beacon-mesh-p2-controller/
├── bridge/
│   ├── state       {"state":"online"} або {"state":"offline"}  [retained/LWT]
│   ├── info        {"version":"9.3.4","adapter":"hci0","transport":"mgmt",...}
│   └── devices     [{"id":"...","friendly_name":"Smart bulb (...)"}]
└── test-lamp/
    ├── ha/set      команди JSON light
    ├── ha/state    локально відомий стан світла
    ├── effect/speed/set
    ├── effect/speed/state
    ├── availability    застаріла сумісна тема (не використовує LWT)
    └── ...              існуючі команди залишаються
```

**Важливо:** LWT тепер працює на `tuya-beacon-mesh-p2-controller/bridge/state`; на нього посилається availability лампи, number і діагностичних сенсорів. Тема `.../<device-id>/availability` залишена для сумісності, але після неочікуваного завершення процесу може залишитися застарілою. Не використовуй її для нових автоматизацій.

Після перезапуску Home Assistant `tuya-beacon-mesh-p2-controller` обробляє `homeassistant/status = online` та повторно публікує discovery і останній відомий стан. Не створюються фіктивні `linkquality` або OTA-сутності: Beacon P2 команди не мають підтвердження від лампи.

Для встановлення: `go build -o tuya-beacon-mesh-p2-controller ./cmd/tuya-beacon-mesh-p2-controller`, після чого `sudo ./tuya-beacon-mesh-p2-controller mqtt`. Наявний `config.json` і `state-*.json` зберігаються без змін. Запуск попередньої версії одночасно з новою заборонений: у брокері обидва клієнти матимуть той самий `client_id`.

### Сумісність і особливості HA

- `device.product_id` продовжує відображатися як `Hardware`, а `device.firmware` — як `Firmware`. Поле `model` для лампи не відправляється за попередньою вимогою.
- Під час міграції Home Assistant може залишити **вручну задані назви**: у такому випадку зміни їх у UI або поверни до назв інтеграції. Перезапуск MQTT не має скидати власні назви користувача.
- Вкладка **MQTT Info** є штатною частиною UI інтеграції Home Assistant, а не додатковою сутністю від `tuya-beacon-mesh-p2-controller`; її не прибирає зміна discovery.
- Структура `bridge/info` і `bridge/devices` **натхненна** Zigbee2MQTT, але не імітує Zigbee-протокол і не сумісна з API Zigbee2MQTT. Команди BLE і MQTT залишилися старими.

---

# tuya-beacon-mesh-p2-controller v9.3.3 — Tuya Product ID as HA Hardware

Home Assistant MQTT Discovery now shows exactly these device details for the verified lamp:

- **Smart bulb (`test-lamp`)**, manufacturer **Tuya**.
- **Connected via tuya-beacon-mesh-p2-controller by AzaOne** (separate bridge device).
- **Firmware: 1.2** (`device.firmware` / MQTT `sw_version`).
- **Hardware: wbl39qpt** (`device.product_id` / MQTT `hw_version`).

The lamp discovery intentionally omits `model` so the Product ID is not shown twice. This UI mapping is a user preference: Tuya calls `wbl39qpt` a *Product ID*, not a hardware revision.

Use the following in your existing `config.json` (keep real keys and the rest of your config untouched):

```json
"device": {
  "name": "Smart bulb",
  "device_id": "test-lamp",
  "product_id": "wbl39qpt",
  "firmware": "1.2",
  "local_key": "YOUR_LOCAL_KEY",
  "beacon_key": "YOUR_BEACON_KEY"
}
```

`device.product_id` takes precedence over the legacy `device.hardware` setting; the old key remains supported to avoid breaking configs. For the known device ID `test-lamp`, missing firmware or product ID uses the previously confirmed values `1.2` and `wbl39qpt`. Unknown device IDs do not get invented version metadata.

Both `light` and `number` keep their existing unique IDs and discovery topics. Only device metadata changes; BLE, Raw HCI, MQTT commands, pacing and key handling are unchanged. Restart `tuya-beacon-mesh-p2-controller mqtt` to republish retained MQTT Discovery. Home Assistant may preserve a manually overridden device name.

---

**Оновлення v9.3.1:** виправлено `HCI filter: invalid argument` для `raw-hci` (структура Linux `hci_filter` має 16 байтів), а `mqtt.go` замінено на наданий користувачем файл із метаданими AzaOne / wbl39qpt. Див. `CHANGES-v9.3.1.md`.

# Tuya Beacon Mesh P2 Controller v9.3 — Raw HCI + Home Assistant Effect Speed

**Нове у v9.3:** незалежний BLE транспорт `raw-hci` і MQTT `number`-сутність **Effect Speed** (1..100%), що змінює байт 4 у `DP73` поточної сцени. Основа v9.2 також збережена: швидкий MGMT-режим з `adv_interval=20ms`, скорочена тривалість MQTT-реклами (`mqtt.tx_duration=250ms`), одна команда `DP73` при перемиканні сцен, JSON-команди HA.

Обидва варіанти передачі мають однаковий P2-шифр, лічильник `seq` і каталог 28 сцен. `mgmt` лишається стандартним і краще співіснує з bluetoothd; `raw-hci` є **опційним** для порівняння реакції лампи з [tuya-beacon-linux](https://github.com/oLucasAguilar/tuya-beacon-linux), який надсилає LE HCI advertising з інтервалом 20ms. `raw-hci` **керує рекламуванням усього адаптера** й може конфліктувати з BlueZ, BLE-периферією та паралельним монітором: для тестів бажано використовувати окремий адаптер. Ні Root, ні дозвіл Bluetooth не додаються жодному іншому процесу: привілейований helper отримує тільки 31-байтові advertising payloads (не ключі).

```bash
# Режим Raw HCI для окремої CLI-команди:
./tuya-beacon-mesh-p2-controller -transport raw-hci -adv-interval 20ms -duration 250ms scene 13

# Постійне використання через MQTT, конфігурація в JSON:
# "controller": {"transport":"raw-hci", "adv_interval":"20ms", "frames":1}
sudo -v
./tuya-beacon-mesh-p2-controller mqtt
```

### Швидкість ефектів у Home Assistant

Публікуються **дві сутності для одного пристрою**: MQTT `light` із 28 ефектами й MQTT `number` — `Effect Speed`. HA light schema не має власного атрибута `effect_speed`, тому швидкість керується окремим числовим повзунком. Обидві сутності матимуть однаковий `device.identifiers`.

```text
homeassistant/number/tuya_p2_<device_id>_effect_speed/config
<topic_prefix>/<device_id>/effect/speed/set     # 1..100
<topic_prefix>/<device_id>/effect/speed/state   # 1..100, або unknown
```

При команді швидкості програма читає останній стан `DP73` і надсилає **один** новий DP73 зі збереженою назвою сцени та яскравістю. Якщо сцена ще не вибрана (режим не `scene`), команда поверне помилку в `/result` замість випадкового ввімкнення непотрібного ефекту. Після перехоплення команд Smart Life (через паралельний `monitor`) значення повзунка оновиться автоматично, коли демон помітить зміну `state`. Стан optimistic/observed, **не** підтвердження від лампи.

```bash
mosquitto_pub -h localhost -t 'tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID/effect/set' -m 'Red & blue gradient'
mosquitto_pub -h localhost -t 'tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID/effect/speed/set' -m '60'
```

**Порада для перевірки лагів:** почни з `-transport mgmt` (він тепер також має `20ms`), далі на окремому адаптері порівняй `raw-hci`. Зміну тривалості можна перевірити через `mqtt.tx_duration`, не плутаючи її з `adv_interval`.

---

# Tuya Beacon Mesh P2 Controller v9.1: MQTT slider queue fix (історія)

**Чому v9 виконував надто багато команд:** MQTT читав наступне повідомлення тільки після повного радіопередавання попереднього (500ms кожне). У HASS повзунок створював чергу застарілих значень. Тепер MQTT reader і радіопередавач розділені; pending queue тримає лише останнє значення на один тип керування.

У `config.json` (секція `mqtt`):

```json
"debounce": "300ms",
"max_wait": "1200ms",
"dedup_window": "2s"
```

- `debounce`: надсилати повзунок після 300ms без нових змін. Power ON/OFF передається без цієї затримки.
- `max_wait`: за тривалого утримання повзунка можна надсилати проміжне значення не частіше за межу очікування; це не гарантована точна частота, бо BLE передавач може бути зайнятий.
- `dedup_window`: повторний однаковий MQTT payload протягом цього вікна після успішного виконання пропускається (коротке вікно, щоб повторна ручна спроба залишалася можливою).

Параметри CLI (перед `mqtt`): `-mqtt-debounce`, `-mqtt-max-wait`, `-mqtt-dedup-window`. `-frames 1` як раніше. MQTT повідомлення читаються окремо від BLE передачі. Блокування `seq`, шифрування та протокол BLE **не змінено**.

MQTT `state`/HA property topics публікуються лише коли стан освітлення змінився, а не під час кожного збільшення лічильника `seq`. Зберігається `last_seq` у самому state файлі; в MQTT `/state` його показано в момент останньої зміни світлових атрибутів.

**Радіоприймання лампою не підтверджується**: `MGMT: active` означає, що адаптер прийняв рекламні налаштування, а не що лампа виконала команду. 500 ms у `controller.duration` залишено для надійності. За потреби можна обережно тестувати 350–400 ms.

---

# tuya-beacon-mesh-p2-controller v9 — керування Tuya Beacon Mesh P2 через Go та BlueZ

Керування власною BLE-лампою **Smart Bulb 2 / Product ID `wbl39qpt` / PHY6230** без Tuya Cloud під час роботи. Відтворює точні 26-байтові P2-кадри з Android Smart Life: `XXTEA-BE(K1) XOR K2`, дві `CRC16HI`, big-endian `src/dst/seq`, контейнер BLE Advertising `02 01 xx 1B 03 + frame`.

**Повна, детальна побайтова специфікація, криптографія та вектори тестування: [PROTOKOL.md](PROTOKOL.md)** (525+ рядків).

## Що нового у v9 — JSON-налаштування, 1 кадр і Home Assistant MQTT Discovery

- **`frames=1` за замовчуванням.** Щоб повернути багаторазове рекламування: `controller.frames: 4` або `-frames 4`.
- Параметри BlueZ, джерела/призначення, `seq`-state, DP, температури, `learn`, QR, Cloud та MQTT читаються з **одного JSON**. Явні прапорці командного рядка мають пріоритет над JSON. Секрети лампи залишаються у `device`.
- MQTT-конфіг сумісний за формою з іншими проєктами (`enabled`, `broker`, `username`, `password`, `client_id`, `topic_prefix`, `ha_discovery_enabled`, `ha_discovery_prefix`).
- **Home Assistant MQTT Discovery** публікує одну `light`-сутність зі станом ON/OFF, яскравістю (1–100%), RGB, температурою (Kelvin) та переліком **усіх 28 сцен**. Це звичайна MQTT-інтеграція, **без окремого кастомного компонента HA**.
- Публікації discovery, станів та availability є **retained**; LWT позначає контролер як `offline` при втраті зв'язку; MQTT-клієнт перепідключається через 5 с.
- Монітор Smart Life може працювати паралельно з MQTT-демоном: демон приблизно раз на 2 с читає `state` і публікує зміни. Це *optimistic/observed state*, не підтвердження виконання команди лампою.
- **Сцена 13 підтверджена в BLE** (`0d00020150640102`, швидкість 80, яскравість 100), більше не вважається реконструйованою.

### Повний приклад конфігурації

У `config.example.json` показано всі JSON-поля. Для реального використання скопіюй його у `config.json`, заміни `device.device_id`, `device.local_key`, `device.beacon_key`, встанови права `chmod 600 config.json`. Не публікуй цей файл з ключами чи паролем MQTT.

```json
{
  "device": {
    "name": "Smart Bulb 2",
    "device_id": "YOUR_DEVICE_ID",
    "local_key": "0123456789abcdef",
    "beacon_key": "000102030405060708090a0b0c0d0e0f"
  },
  "controller": {
    "adapter": "hci0", "src": "0x7a59", "phone_src": "0x7a59",
    "dst": "0x0003", "transport": "mgmt", "frames": 1,
    "multi_dp": true, "duration": "500ms", "gap": "120ms",
    "flags": "01", "kelvin_min": 2700, "kelvin_max": 6500,
    "invert_temp": false, "scan": true
  },
  "mqtt": {
    "enabled": true,
    "broker": "tcp://localhost:1883",
    "username": "", "password": "",
    "client_id": "tuya-beacon-mesh-p2-controller",
    "topic_prefix": "tuya-beacon-mesh-p2-controller",
    "ha_discovery_enabled": true,
    "ha_discovery_prefix": "homeassistant"
  }
}
```

Можна так само використати `"topic_prefix": "bledom"`, однак **не запускай два різні контролери з одним `client_id`**. Шлях до `state` можна вказати `controller.state`; типовий шлях — `./state/state-<device-id>.json`.

Запуск із будь-якого каталогу:

```sh
./tuya-beacon-mesh-p2-controller -config config.json scene list
./tuya-beacon-mesh-p2-controller -config config.json -dry-run on    # лише 1 frame, без радіо
./tuya-beacon-mesh-p2-controller -config config.json on
./tuya-beacon-mesh-p2-controller -config config.json -frames 4 on  # CLI перекриває JSON
./tuya-beacon-mesh-p2-controller -config config.json mqtt         # довготривалий процес
```

`mqtt.enabled=false` забороняє запуск MQTT-режиму; інші CLI-команди працюватимуть. Не потрібно тримати `mqtt` процес запущеним для простого CLI `on/off`.

### MQTT / Home Assistant

Якщо `mqtt.topic_prefix = "tuya-beacon-mesh-p2-controller"`, а ID лампи — `YOUR_DEVICE_ID`, то піддерево буде `tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID`. `ha_discovery_prefix` зазвичай `homeassistant`.

| Topic (відносно `tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID`) | Значення | Напрям |
| --- | --- | --- |
| `power/set` / `power/state` | `ON` або `OFF` | команда / стан |
| `brightness/set` / `brightness/state` | 1–100 | команда / стан |
| `color/set` / `color/state` | `R,G,B` (0–255) | команда / стан |
| `temperature/set` / `temperature/state` | Кельвіни | команда / стан |
| `effect/set` / `effect/state` | назва сцени зі списку 28 | команда / стан |
| `effect/speed/set` / `effect/speed/state` | швидкість 1–100%, зміна DP73 | команда / стан |
| `set` | старі текстові команди `rgb 10 20 30`, `scene 8` тощо | команда |
| `state` / `result` | JSON (останній `seq`, optimistic state / результат передачі) | стан / подія |
| `availability` | `online` / `offline` | доступність MQTT-контролера |

HA Discovery публікується як **retained** у `homeassistant/light/tuya_p2_YOUR_DEVICE_ID/config` і `homeassistant/number/tuya_p2_YOUR_DEVICE_ID_effect_speed/config`. Вимкнення `ha_discovery_enabled` публікує порожній retained payload, щоб прибрати стару сутність. HA має бути підключений до того самого Mosquitto і мати увімкнений MQTT Discovery.

Приклади ручних команд:

```sh
mosquitto_pub -h localhost -t 'tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID/power/set' -m ON
mosquitto_pub -h localhost -t 'tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID/color/set' -m '100,232,111'
mosquitto_pub -h localhost -t 'tuya-beacon-mesh-p2-controller/YOUR_DEVICE_ID/effect/set' -m 'Red & blue gradient'
```

**Увага:** канал `tcp://` не шифрує MQTT. Використовуй захищену локальну мережу; для зовнішнього підключення потрібний TLS-шлюз/проксі. Last Will відображає працездатність MQTT-демона, **не фізичну доступність Bluetooth-лампи**. Доступний стан не означає отримане підтвердження від лампи.

## Що нового у v7

- **`tuya-beacon-mesh-p2-controller login` — як у [oLucasAguilar/tuya-beacon-linux](https://github.com/oLucasAguilar/tuya-beacon-linux):** авторизація через QR-код, **User Code** з Smart Life, без Tuya Developer, IoT Core, Access ID або Access Secret. Реалізовано **безпосередньо на Go**, з використанням протоколу офіційного Tuya Device Sharing SDK. Для малювання QR у терміналі потрібна лише `qrencode`.
- Авторизаційна сесія живе лише в оперативній пам'яті, не записується на диск. Запит вибирає **точний `device_id`** із конфігурації, а не назву лампи. Отриманий K1 за замовчуванням оновлює `device.local_key` приватним атомарним записом, з резервною копією і очищенням K2 тільки якщо K1 реально змінився. `-qr-save=false` — без змін файлів.
- `cloud local-key` з попередньої версії збережено як запасний варіант через OpenAPI; звичайна робота лампи не залежить від інтернету.

## Що було у v6

- **Живе навчання на ПК:** `learn live` слухає BLE Advertising Smart Life через `hci0`/`hci1`, сам визначає `src/dst` (за криптографічною перевіркою) й відновлює `beacon_key` після декількох ON/OFF. **Android BTSnoop необов'язковий**. Телефон має бути в зоні приймання Bluetooth-адаптера ПК; це не читання HCI-журналу телефона.
- **Отримання `local_key`:** `cloud local-key` використовує офіційний Tuya OpenAPI, HMAC-SHA256 і `TUYA_CLIENT_ID` / `TUYA_CLIENT_SECRET` зі змінних середовища. За `-cloud-save` оновлює приватний JSON-конфіг, створює резервну копію і очищає старий `beacon_key` після ротації K1. Без `-cloud-save` тільки підтверджує отримання ключа, не показуючи його.
- `learn capture.cfa` збережено для офлайн-файлів; для обох режимів `-learn-out FILE` створює окремий приватний результат (0600). Навчання не перезаписує конфіг автоматично.

## Можливості попередніх версій

- **Один довготривалий BlueZ MGMT socket** замість запуску `sudo btmgmt` для кожного кадру. Програма шукає **вільний** рекламний інстанс (`0x003D`), додає/знімає лише його (`0x003E`/`0x003F`). Старий `-transport btmgmt` залишено як fallback.
- **Multi-DP:** `rgb`, `temp` і `white` зазвичай вміщуються в **один** 16-байтовий блок; за замовчуванням **один кадр на блок** (`frames=1`). Експериментальна оптимізація: вимикається `-multi-dp=false`, бо фізична лампа ще не перевірялася з об'єднаними DP.
- **Монітор** приймає P2 з телефона, розбирає *декілька* DP у кадрі та записує актуальний `seq` і **optimistic** стан (`confirmed:false`) у JSON. Команди іншого джерела слухаються окремим `-phone-src`.
- **Захист від повторів та перегонів:** `seq` резервується до передачі; короткий файловий lock на state, додатковий `.tx.lock` на весь batch, щоб конкуруючі CLI-виклики не переставляли кадри.
- **Навчання `beacon_key`:** `learn capture.cfa` відновлює постійну XOR-маску з відомих ON/OFF через дійсний `local_key` і перевіряє незалежні кадри; `-learn-out` зберігає результат лише до **нового** файлу `0600`.
- **Перевірена синхронізація з BTSnoop**: `seq sync-log` підвищує лічильник лише за кадрами, які успішно розшифровані з обома CRC.
- **MQTT bridge** (включно з HA MQTT Discovery у v9), retained станами, LWT, стандартними текстовими командами та JSON; без окремого Python/HA-компонента. Тільки локальна LAN.
- `./state/state-<device-id>.json` у робочому каталозі запуску; секретні ключі не записуються в state.
- **Linux amd64 та arm64**, лише стандартна бібліотека Go; `tinygo.org/x/bluetooth` не потрібний: його Linux API не дозволяє стабільно встановити raw 31-byte ADV.

## Залежності та збирання

На Arch Linux:

```sh
sudo pacman -S --needed go bluez bluez-utils qrencode
sudo systemctl enable --now bluetooth

go test ./...
go vet ./...
go build -trimpath -o tuya-beacon-mesh-p2-controller ./cmd/tuya-beacon-mesh-p2-controller
go build -trimpath -o tuya-beacon-mesh-p2-controller-server ./cmd/tuya-beacon-mesh-p2-controller-server
```

На Armbian/Debian:

```sh
sudo apt install golang-go bluez qrencode
# потрібен Go >= 1.23

go build -trimpath -o tuya-beacon-mesh-p2-controller ./cmd/tuya-beacon-mesh-p2-controller
go build -trimpath -o tuya-beacon-mesh-p2-controller-server ./cmd/tuya-beacon-mesh-p2-controller-server
```

Заздалегідь переконайся, що Bluetooth адаптер увімкнений: `bluetoothctl show` або `btmgmt info`.

### Отримання `local_key` через QR Smart Life (рекомендовано)

1. У Smart Life відкрий **Me → Settings → Account and Security → User Code**. Це не пароль.
2. У терміналі (без `sudo`):

```sh
tuya-beacon-mesh-p2-controller login
# або вказати код одразу:
tuya-beacon-mesh-p2-controller -qr-user-code YOUR_SMART_LIFE_USER_CODE login
# якщо код хочеш перевірити без оновлення конфігурації:
tuya-beacon-mesh-p2-controller -qr-save=false login
```

3. Відскануй QR у Smart Life: **+ → Scan** та підтвердь доступ. Застосунок може показувати **Home Assistant**, оскільки використовується публічна реєстрація її інтеграції в Tuya. Підтверджуй лише QR, який щойно сам запустив.
4. Програма запитує `local_key` лише для ID, указаного в `device.device_id` (або `-cloud-device-id`), і, якщо збереження увімкнено, оновлює JSON у режимі `0600` із резервною копією. Ключ **не друкується**.

Приклад файлу-конфіга до `login` (K2 можна додати пізніше через `learn live`):

```json
{
  "device": {
    "device_id": "YOUR_DEVICE_ID",
    "local_key": "",
    "beacon_key": ""
  }
}
```

У разі зміни `local_key` старий `beacon_key` очищається — його потрібно відновити заново. Якщо Tuya Device Sharing API не повертає ключ Bluetooth-пристрою, програма повідомить про це без запису некоректного значення. Вебавторизація потребує інтернету **тільки під час отримання ключа**.

### Альтернатива: `local_key` через Tuya Developer OpenAPI

Після прив'язки Smart Life до Cloud-проєкту з активним **IoT Core** використовуй **Access ID** і **Access Secret** цього проєкту. Не записуй Secret у `.bash_history`, JSON, аргументи командного рядка або логи.

```sh
export TUYA_CLIENT_ID="YOUR_PROJECT_ACCESS_ID"
read -rsp "Tuya Access Secret: " TUYA_CLIENT_SECRET; echo
export TUYA_CLIENT_SECRET

# Для старого акаунта Smart Life в Central Europe:
tuya-beacon-mesh-p2-controller -cloud-region eu -cloud-save cloud local-key
# Якщо акаунт у Western Europe, заміни eu на we.
```

Команда бере `device_id` із конфіга. Якщо файл ще не створено, додай `-cloud-device-id YOUR_DEVICE_ID`. За `-cloud-save` новий ключ записується з правами 0600; якщо він змінився, попередній `beacon_key` очищається. Старий конфіг зберігається в `.bak-...`. Мережа потрібна **лише** для цього запиту, керування лампою залишається офлайновим.

Після цього увімкни Bluetooth на ПК, запусти `sudo -v`, розмісти телефон поруч і натискай ON/OFF у Smart Life:

```sh
sudo -v
tuya-beacon-mesh-p2-controller -adapter hci0 -learn-out beacon-key.json learn live
# За потреби довше слухати:
tuya-beacon-mesh-p2-controller -learn-timeout 5m -learn-out beacon-key.json learn live
# Якщо сканування вже запущене іншою програмою:
tuya-beacon-mesh-p2-controller -scan=false -learn-out beacon-key.json learn live
```

За успіху отриманий `beacon_key` записується в окремий `beacon-key.json` (`0600`); **вручну** внеси поле `beacon_key` до основного конфіга. Якщо після прив'язки змінилися адреси, програма також покаже актуальні `src/dst`, і їх слід вказати параметрами `-phone-src/-dst` (за потреби `-src`). Наявний `monitor` після цього автоматично синхронізуватиме `seq` із командами телефона.

`learn live` потребує актуальний `local_key` і кадри ON/OFF, але **не потребує старого `beacon_key`**. Пасивний BLE-сніфінг не розкриває сам `local_key`; для цього рекомендовано `tuya-beacon-mesh-p2-controller login` (QR Smart Life). Запасний OpenAPI-спосіб `cloud local-key` може повертати помилку `28841002`, якщо закінчився IoT Core.

### Налаштування

Розмісти поруч із `tuya-beacon-mesh-p2-controller` конфігурацію `tuya-lamp-re-verified.json`, або в `~/.config/tuya-beacon-mesh-p2-controller/config.json`, або явно вибери `-config file.json`:

```json
{
  "device": {
    "device_id": "YOUR_DEVICE_ID",
    "local_key": "YOUR_16_ASCII_CHAR_LOCAL_KEY",
    "beacon_key": "YOUR_32_HEX_CHAR_BEACON_KEY"
  }
}
```

Цей приклад — **схема**, не готовий файл: `local_key` має рівно **16 ASCII-символів**, `beacon_key` — **32 hex-символи** (16 байтів). Ніколи не коміть реальні ключі до Git. Постав `chmod 600 tuya-lamp-re-verified.json`.

`tuya-beacon-mesh-p2-controller` сам шукає конфіг поруч із **реальним виконуваним файлом** (із розв'язанням symlink), а також у CWD і `~/.config/tuya-beacon-mesh-p2-controller`.

### Перший запуск

```sh
sudo -v                              # дозвіл на вузький root MGMT helper
./tuya-beacon-mesh-p2-controller seq show                   # прочитати останній seq
./tuya-beacon-mesh-p2-controller seq sync 0x0092            # ПРИКЛАД: встановити реально останній seq
./tuya-beacon-mesh-p2-controller -dry-run on                # НІЧОГО не передає і не змінює seq
./tuya-beacon-mesh-p2-controller off                        # передає негайно
./tuya-beacon-mesh-p2-controller on
```

`seq sync` із `0x0092` — **лише приклад**, а не відомий поточний лічильник. Якщо користувався Smart Life або старою CLI версією пізніше, синхронізуй із **найсвіжішим** `seq`. Не намагайся примусово знизити лічильник.

### Встановлення в PATH

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 ./tuya-beacon-mesh-p2-controller "$HOME/.local/bin/tuya-beacon-mesh-p2-controller"
install -m 600 /secure/path/tuya-lamp-re-verified.json "$HOME/.local/bin/tuya-lamp-re-verified.json"
command -v tuya-beacon-mesh-p2-controller
```

Файли `state-*.json`, `state-*.json.lock`, `state-*.json.tx.lock` зберігаються у `./state/` відносно робочого каталогу. Власний файл можна задати через `-state /writable/path/state.json`.

## CLI

```sh
tuya-beacon-mesh-p2-controller on
tuya-beacon-mesh-p2-controller off

tuya-beacon-mesh-p2-controller brightness 50           # 1..100%, DP3=10..1000
tuya-beacon-mesh-p2-controller brightness-raw 510

tuya-beacon-mesh-p2-controller temp 4000               # Kelvin -> DP4 умовна шкала
tuya-beacon-mesh-p2-controller temp 5000
tuya-beacon-mesh-p2-controller temp-raw 500            # точне значення DP4 (0..1000)
tuya-beacon-mesh-p2-controller white 4000 80           # white mode + DP4 + DP3

tuya-beacon-mesh-p2-controller rgb 100 232 111        # перетворення RGB -> HSV, DP2 + DP11
tuya-beacon-mesh-p2-controller hsv 120 100 100

tuya-beacon-mesh-p2-controller mode white
tuya-beacon-mesh-p2-controller mode colour
tuya-beacon-mesh-p2-controller mode scene            # тільки DP2 (режим)
tuya-beacon-mesh-p2-controller scene 8               # один DP73: вибір конкретної сцени (v9.2+)
tuya-beacon-mesh-p2-controller scene list            # всі 28 назв і RAW
tuya-beacon-mesh-p2-controller scene "Full color gradient" --speed 75 --brightness 100
tuya-beacon-mesh-p2-controller scene current         # остання збережена сцена
tuya-beacon-mesh-p2-controller mode music              # експеримент, НЕ підтверджено

tuya-beacon-mesh-p2-controller raw 0241010b0400786464   # два TLV у одному plaintext (експеримент)
tuya-beacon-mesh-p2-controller dp bool 7 on               # експериментальні DP
tuya-beacon-mesh-p2-controller dp enum 2 1
tuya-beacon-mesh-p2-controller dp value 3 500
tuya-beacon-mesh-p2-controller dp raw 11 00786464
```

**Температура**: відомий тільки масштаб DP4 `0..1000`; типова конфігурація `-kelvin-min 2700 -kelvin-max 6500` не є виміряною характеристикою цієї лампи. Калібруй реальні межі або користуйся `temp-raw`. Для іншого напряму зміни теплого/холодного `-invert-temp`.

**Режими**: для mode=0/1/2 у HCI-захопленні є незалежні кадри. `music` (mode=3) описано в Tuya Device Model, але не випробувано нашою CLI. Для DP73 є всі 28 RAW-шаблонів, підтверджених перехопленнями Smart Life. CLI-передавання кожного конкретного ефекту потребує фізичної перевірки.

**Кілька DP за раз**: `rgb` (DP2+DP11) та `temp` (DP2+DP4) — **один 16B блок**, `white` (DP2+DP4+DP3) — один 15B блок. За `-frames 4` передаються чотири advertising-пакети з унікальними seq. Якщо оптимізований multi-DP не спрацює на твоїй лампі:

```sh
tuya-beacon-mesh-p2-controller -multi-dp=false rgb 100 232 111
tuya-beacon-mesh-p2-controller -multi-dp=false white 4000 80
```

Для перевірки **без** BLE:

```sh
tuya-beacon-mesh-p2-controller -dry-run rgb 100 232 111
tuya-beacon-mesh-p2-controller -frames 3 -duration 900ms -gap 100ms on
tuya-beacon-mesh-p2-controller -transport btmgmt -multi-dp=false -duration 900ms off  # старий перевірений шлях
```

`-duration` — **час активної реклами на кадр**, не інтервал між радіо-пакетами. BlueZ/контролер визначає внутрішній рекламний інтервал. `MGMT Added` не означає, що лампа фізично виконала команду.

## Прив'язка, seq, state

```sh
tuya-beacon-mesh-p2-controller seq show
tuya-beacon-mesh-p2-controller state show
tuya-beacon-mesh-p2-controller seq sync 0x0100
tuya-beacon-mesh-p2-controller seq sync-log /path/to/phone.cfa
```

`sync-log` підтримує стандартний Android Bluetooth BTSnoop **H4** з TX opcode `0x2037`. Відхиляє сторонні пакети без правильного `local_key`, `beacon_key`, двох CRC і DP. Із новим спарюванням можуть змінитися `local_key`, `beacon_key`, `src` та `dst`. Для такого випадку потрібен новий конфіг і **окремий** state-файл, інакше перевірка fingerprint навмисно зупинить запуск.

Для відстеження Smart Life у першому терміналі:

```sh
sudo -v
tuya-beacon-mesh-p2-controller monitor
# або окремий адаптер:
tuya-beacon-mesh-p2-controller -adapter hci1 monitor
# якщо сканування вже працює:
tuya-beacon-mesh-p2-controller -scan=false monitor
```

У другому:

```sh
tuya-beacon-mesh-p2-controller seq show
tuya-beacon-mesh-p2-controller state show
tuya-beacon-mesh-p2-controller on
```

Монітор використовує Bluetooth HCI Monitor, фільтрацію за `-phone-src` та `-dst`, перевірку криптографії і CRC. Файл `state` зберігає також `light.power`, `light.mode`, `light.brightness_raw`, `light.temperature_raw`, `light.hsv`, `light.last_source`, `light.updated_at`, `light.confirmed=false` — тобто **останню отриману/надіслану команду**, не гарантований фізичний стан лампи.

У разі одночасного scan/advertise на одному контролері можливі пропуски та конфлікти. Монітор не перехоплює секретні дані телефону з повітря і не бачить усіх дій, якщо Bluetooth-сканер їх пропустив.

## Відновлення beacon_key після повторного спарювання

Зі свіжим `local_key` в конфігу (поле `beacon_key` для **самого learn** може бути відсутнім) працює **або** `learn live` з радіозахопленням ПК, **або** `.cfa` із командами ON/OFF:

```sh
tuya-beacon-mesh-p2-controller -config ./config.json -learn-out ./recovered-key.json learn live
# альтернативно захоплення Android:
tuya-beacon-mesh-p2-controller -config ./config.json learn phone.cfa
# після підтвердження:
tuya-beacon-mesh-p2-controller -config ./config.json -learn-out ./recovered-key.json learn phone.cfa
```

Результат JSON містить `beacon_key`, `source`, `destination`, лічильники перевірених кадрів. **Вручну** перенеси `beacon_key` в основний конфіг. CLI не друкує секрет у консоль і не перезаписує вже наявний файл.

Відомий plaintext дає `beacon_key[i] = captured_frame[9+i] XOR XXTEA_BE(known_plaintext,K1)[i]`. Утиліта перевіряє отриманий кандидат не лише на ON/OFF, але й на інших зашифрованих DP та обох CRC. Старі ключі після репарингу можуть не підходити. Для деталей див. [PROTOKOL.md](PROTOKOL.md).

## MQTT і Home Assistant Discovery

Актуальна конфігурація й таблиця тем наведені в розділі v9 на початку документа. Залишено старий `<prefix>/set` для текстових команд, а для Home Assistant додано окремі теми живлення, яскравості, RGB, колірної температури та ефектів. Стан публікується без підтвердження від лампи. Дозволені тільки команди освітлення, виконання довільного shell-коду не підтримується.


## Перевірки

Перевірено в робочому середовищі без BLE-радіо:

- Go `go test ./...`, `go test -race ./...`, `go vet ./...`.
- Синтетичні вектори XXTEA+XOR+CRC і розбір multi-DP.
- Приватний regression test з ключами й файлом від 20.11.2026: **84/84** коректні P2 кадри формуються побайтово так само, як у HCI журналі. У `learn` ключ відновлюється й проходить перевірку на **84 кадрах і 13 різних DP payload**.

**Поки не перевірено фізично:** новий MGMT-передавач на твоєму конкретному Bluetooth-контролері, реальне виконання *об'єднаних* DP лампою, приймання нового `src` та MQTT/monitor у тривалому одночасному режимі. Для надійного першого тесту запусти:

```sh
tuya-beacon-mesh-p2-controller -transport btmgmt -multi-dp=false -duration 900ms off
# потім:
tuya-beacon-mesh-p2-controller -multi-dp=false on
# і після цього:
tuya-beacon-mesh-p2-controller rgb 100 232 111
```

Якщо є `btmon`, перевір фактичну HCI рекламу (а не лише успішну відповідь MGMT):

```sh
sudo btmon
```

**Поточні обмеження:** нема confirmed lamp ACK, нема повного парсера pairing/status P2, нема загальної підтримки різних ламп без профілів; за межами 16-бітного `seq` програма зупиняється. Див. §17 в `PROTOKOL.md`.

## Сумісність і межі v7

- QR-авторизація `login`, підписи, AES-GCM та отримання ключа перевірені локальними HTTP-тестами й векторами за кодом офіційного SDK, **але не перевірялися проти твого реального Tuya-акаунта**. Аналогічно OpenAPI — лише локальний HTTP-тест.
- Парсер живого захоплення використовує **HCI LE Advertising Report** (`0x3e/0x02` або `0x3e/0x0d`). Він не декодує напряму обмін застосунку з Tuya Cloud і не читає приватний журнал HCI телефона.
- Живе навчання не перевірено на реальному Bluetooth-адаптері в цій збірці; алгоритм `learn` повторно перевірено на раніше записаних кадрах Tuya P2.
- Старий `state` при зміні ключа може відхилятися перевіркою fingerprint: це навмисний захист. Після зміни ключів збережи резервну копію старого state і синхронізуй новий; не примушуй записувати нижчі `seq`.

Джерело реалізації QR: [tuya-beacon-linux/cloud.py](https://github.com/oLucasAguilar/tuya-beacon-linux/blob/main/tuyabeacon/cloud.py), [Tuya Device Sharing SDK](https://github.com/tuya/tuya-device-sharing-sdk).

Документація OpenAPI: [Query Device Details](https://developer.tuya.com/en/docs/cloud/3829469013?id=Kcp2l2v9wma0m) і [Sign Requests](https://developer.tuya.com/en/docs/iot/new-singnature?id=Kbw0q34cs2e5g).
