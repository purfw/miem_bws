import { useMemo, useState } from "react";
import type { ConferenceSection, ScheduleSlot } from "../../types/conference";
import { Button } from "../ui/Button/Button";
import { dateLocale, localize, tr } from "../../i18n/runtime";
import { parseDate } from "../../i18n/dates";
import styles from "./ProgramView.module.css";
import { sessionTypeLabel } from "./sessionTypes";

interface ProgramViewProps {
  schedule: ScheduleSlot[];
  sections: ConferenceSection[];
  /** Renders a "join" button for each slot when set. */
  onJoin?: (slotId: string) => void;
  joinBusy?: string;
}

const NO_SECTION = "";

/** Programme grouped by day and section, with a text filter and section/day selectors. */
export function ProgramView({ schedule, sections, onJoin, joinBusy }: ProgramViewProps) {
  const [query, setQuery] = useState("");
  const [day, setDay] = useState("");
  const [section, setSection] = useState("all");

  const days = useMemo(() => [...new Set(schedule.map((slot) => slot.slot_date))].sort(), [schedule]);
  const sectionTitles = useMemo(() => new Map(sections.map((item) => [item.id, localize(item.title, item.title_en)])), [sections]);
  const sectionOrder = useMemo(() => new Map(sections.map((item, index) => [item.id, item.sort_order * 1000 + index])), [sections]);

  const filtered = useMemo(() => {
    const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    return schedule
      .filter((slot) => !day || slot.slot_date === day)
      .filter((slot) => section === "all" || (slot.section_id ?? NO_SECTION) === section)
      .filter((slot) => {
        if (!words.length) return true;
        const text = [slot.title, slot.speaker_name, slot.speaker_affiliation, slot.description, slot.room, slot.section_title]
          .filter(Boolean).join(" ").toLowerCase();
        return words.every((word) => text.includes(word));
      })
      .sort((a, b) => a.slot_date.localeCompare(b.slot_date) || a.start_time.localeCompare(b.start_time) || a.title.localeCompare(b.title));
  }, [schedule, day, section, query]);

  if (!schedule.length) return <p>{tr("Программа ещё не опубликована.", "The programme has not been published yet.")}</p>;

  const byDay = new Map<string, Map<string, ScheduleSlot[]>>();
  for (const slot of filtered) {
    const sections = byDay.get(slot.slot_date) ?? new Map<string, ScheduleSlot[]>();
    const key = slot.section_id ?? NO_SECTION;
    sections.set(key, [...(sections.get(key) ?? []), slot]);
    byDay.set(slot.slot_date, sections);
  }
  const hasSections = schedule.some((slot) => slot.section_id);

  return (
    <div className={styles.program}>
      <div className={styles.filters}>
        <label className={styles.search}>
          <span className={styles.visuallyHidden}>{tr("Поиск по программе", "Search the programme")}</span>
          <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={tr("Доклад, докладчик, аудитория…", "Talk, speaker, room…")} />
        </label>
        {days.length > 1 && (
          <label>
            <span className={styles.visuallyHidden}>{tr("День", "Day")}</span>
            <select value={day} onChange={(event) => setDay(event.target.value)}>
              <option value="">{tr("Все дни", "All days")}</option>
              {days.map((item) => <option key={item} value={item}>{formatDay(item)}</option>)}
            </select>
          </label>
        )}
        {hasSections && (
          <label>
            <span className={styles.visuallyHidden}>{tr("Секция", "Section")}</span>
            <select value={section} onChange={(event) => setSection(event.target.value)}>
              <option value="all">{tr("Все секции", "All sections")}</option>
              {sections.map((item) => <option key={item.id} value={item.id}>{localize(item.title, item.title_en)}</option>)}
              {schedule.some((slot) => !slot.section_id) && <option value={NO_SECTION}>{tr("Общая программа", "General programme")}</option>}
            </select>
          </label>
        )}
      </div>

      {filtered.length === 0 && <p className={styles.empty}>{tr("Ничего не найдено. Измените условия поиска.", "Nothing found. Try other filters.")}</p>}

      {[...byDay.entries()].map(([date, groups]) => (
        <section key={date} className={styles.day}>
          <h3 className={styles.dayTitle}>{formatDay(date)}</h3>
          {[...groups.entries()]
            .sort(([a], [b]) => (sectionOrder.get(a) ?? Number.MAX_SAFE_INTEGER) - (sectionOrder.get(b) ?? Number.MAX_SAFE_INTEGER))
            .map(([key, slots]) => (
              <div key={key || "general"} className={styles.group}>
                {hasSections && <h4 className={styles.sectionTitle}>{key ? sectionTitles.get(key) ?? slots[0].section_title : tr("Общая программа", "General programme")}</h4>}
                <ol className={styles.slots}>
                  {slots.map((slot) => (
                    <li key={slot.id} className={styles.slot}>
                      <time className={styles.time}>{slot.start_time.slice(0, 5)}–{slot.end_time.slice(0, 5)}</time>
                      <div className={styles.slotBody}>
                        <span className={styles.type}>{sessionTypeLabel(slot.session_type)}{slot.room ? ` · ${slot.room}` : ""}</span>
                        <strong className={styles.slotTitle}>{slot.title}</strong>
                        {slot.speaker_name && <span className={styles.speaker}>{slot.speaker_name}{slot.speaker_affiliation ? `, ${slot.speaker_affiliation}` : ""}</span>}
                        {slot.description && <p className={styles.description}>{slot.description}</p>}
                      </div>
                      {onJoin && slot.session_type !== "break" && (
                        <Button variant="secondary" disabled={joinBusy === slot.id} onClick={() => onJoin(slot.id)}>
                          {joinBusy === slot.id ? tr("Проверяем доступ…", "Checking access…") : tr("Подключиться", "Join session")}
                        </Button>
                      )}
                    </li>
                  ))}
                </ol>
              </div>
            ))}
        </section>
      ))}
    </div>
  );
}

function formatDay(date: string): string {
  return parseDate(date).toLocaleDateString(dateLocale(), { weekday: "long", day: "numeric", month: "long" });
}
