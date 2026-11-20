import { tr } from "../../i18n/runtime";

export const sessionTypes = ["talk", "keynote", "workshop", "panel", "poster", "break"];

export function sessionTypeLabel(type: string | undefined): string {
  switch (type ?? "talk") {
    case "talk": return tr("Доклад", "Talk");
    case "keynote": return tr("Пленарный доклад", "Keynote");
    case "workshop": return tr("Мастер-класс", "Workshop");
    case "panel": return tr("Дискуссия", "Panel");
    case "poster": return tr("Постер", "Poster");
    case "break": return tr("Перерыв", "Break");
    default: return type ?? "";
  }
}
