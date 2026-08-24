// Раскладывает даты расходов демо-групп по последнему месяцу.
//
// Сервер ставит create_at сам (api.Operation.CreateAt), поля в API нет — а на
// скриншоте «Итоги» с графиками «Траты по дням» и «По дням недели» все расходы
// одной датой дают один столбик вместо картины месяца. Правим прямо в базе:
// данные локальные и существуют только ради витрины.
//
//   docker exec splitty-app-mongo-1 mongosh splitty --quiet --file /tmp/backdate.js
const ROOMS = ["Поездка в Стамбул", "Квартира на Тверской", "Дача на выходные",
               "Trip to Lisbon", "Flat share", "Weekend cabin",
               "Viaje a Barcelona", "Piso compartido", "Finca el finde",
               "Städtetrip Berlin", "WG Prenzlauer Berg", "Wochenende am See",
               "Week-end à Lyon", "Coloc rue Vieille", "Chalet en montagne"];

// Дни назад для i-й операции комнаты. Не равномерно: реальные траты идут
// сгустками — поездка за несколько дней, квартира растянута на месяц.
const OFFSETS = [27, 26, 26, 25, 23, 22, 20, 14, 11, 9, 6, 4, 3, 2, 1, 0];

let touched = 0;
for (const name of ROOMS) {
  const room = db.room.findOne({ name: name });
  if (!room || !room.operations) { print("нет группы: " + name); continue; }
  const ops = room.operations;
  for (let i = 0; i < ops.length; i++) {
    const daysAgo = OFFSETS[i % OFFSETS.length];
    const at = new Date(Date.now() - daysAgo * 86400000 - (i * 3600000));
    ops[i].create_at = at;
    touched++;
  }
  db.room.updateOne({ _id: room._id }, { $set: { operations: ops } });
  print(name + ": " + ops.length + " операций разложено");
}
print("итого сдвинуто: " + touched);
