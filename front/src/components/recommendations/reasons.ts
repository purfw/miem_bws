import type { RecommendationReason } from "../../types/conference";
import { tr } from "../../i18n/runtime";

export function reasonLabel(reason: RecommendationReason): string {
  switch (reason.code) {
    case "topic":
      return reason.value ?? tr("Похожая тема", "Similar topic");
    case "similar_text":
      return tr("Похожая тематика", "Similar subject");
    case "co_attendance":
      return tr("Выбирают участники похожих событий", "Chosen by attendees of similar events");
    case "city":
      return reason.value ? tr(`Тоже в городе ${reason.value}`, `Also in ${reason.value}`) : tr("Тот же город", "Same city");
    case "popular":
      return tr("Популярно", "Popular");
    case "registration_open":
      return tr("Регистрация открыта", "Registration open");
  }
}
