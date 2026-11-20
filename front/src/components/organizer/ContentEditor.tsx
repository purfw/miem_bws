import { useEffect, useState } from "react";
import { readableError } from "../../api/client";
import { useAuth } from "../../auth/useAuth";
import { Button } from "../ui/Button/Button";
import { tr } from "../../i18n/runtime";
import page from "../../pages/Page.module.css";
import styles from "./ContentEditor.module.css";

interface Section { id?: string; title: string; title_en: string; description: string; moderator_name: string; moderator_affiliation: string }
interface Person { id?: string; name: string; kind: "organizer" | "committee"; role: string; affiliation: string; job_title: string; photo_url: string }
interface Partner { id?: string; name: string; logo_url: string; website_url: string; tier: string }

interface ContentResponse {
  sections: Array<{ id: string; title: string; title_en?: string | null; description?: string | null; moderator?: { name: string; affiliation?: string | null } | null }>;
  organizers: Array<{ id: string; name: string; kind: "organizer" | "committee"; role: string; affiliation?: string | null; job_title?: string | null; photo_url?: string | null }>;
  partners: Array<{ id: string; name: string; logo_url?: string | null; website_url?: string | null; tier?: string | null }>;
}

type Kind = "sections" | "organizers" | "partners";

const emptySection: Section = { title: "", title_en: "", description: "", moderator_name: "", moderator_affiliation: "" };
const emptyPerson: Person = { name: "", kind: "organizer", role: "", affiliation: "", job_title: "", photo_url: "" };
const emptyPartner: Partner = { name: "", logo_url: "", website_url: "", tier: "" };

const toSections = (data: ContentResponse): Section[] => data.sections.map((s) => ({ id: s.id, title: s.title, title_en: s.title_en ?? "", description: s.description ?? "", moderator_name: s.moderator?.name ?? "", moderator_affiliation: s.moderator?.affiliation ?? "" }));
// "organizer" is the default role and is shown as an empty field.
const toPeople = (data: ContentResponse): Person[] => data.organizers.map((p) => ({ id: p.id, name: p.name, kind: p.kind, role: p.role === "organizer" ? "" : p.role, affiliation: p.affiliation ?? "", job_title: p.job_title ?? "", photo_url: p.photo_url ?? "" }));
const toPartners = (data: ContentResponse): Partner[] => data.partners.map((p) => ({ id: p.id, name: p.name, logo_url: p.logo_url ?? "", website_url: p.website_url ?? "", tier: p.tier ?? "" }));

/** Optional text: blank values are omitted so the server stores NULL. */
function opt(value: string): string | undefined {
  return value.trim() || undefined;
}

function move<T>(items: T[], index: number, delta: number): T[] {
  const next = [...items];
  const target = index + delta;
  if (target < 0 || target >= next.length) return items;
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}

/** Editors for a conference's sections, committees and partners. */
export function ContentEditor({ conferenceId, onSaved }: { conferenceId: string; onSaved?: () => void }) {
  const { request } = useAuth();
  const [sections, setSections] = useState<Section[]>([]);
  const [people, setPeople] = useState<Person[]>([]);
  const [partners, setPartners] = useState<Partner[]>([]);
  const [loaded, setLoaded] = useState("");
  const [busy, setBusy] = useState<Kind | "">("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    request<ContentResponse>(`/my/conferences/${conferenceId}/content`, { signal: controller.signal })
      .then((data) => {
        if (controller.signal.aborted) return;
        setSections(toSections(data));
        setPeople(toPeople(data));
        setPartners(toPartners(data));
        setLoaded(conferenceId);
      })
      .catch((reason: unknown) => { if (!controller.signal.aborted) setError(readableError(reason)); });
    return () => controller.abort();
  }, [conferenceId, request]);

  async function save(kind: Kind) {
    setBusy(kind); setError(""); setMessage("");
    const data = kind === "sections"
      ? sections.map((s) => ({ id: s.id, title: s.title, title_en: opt(s.title_en), description: opt(s.description), moderator_name: opt(s.moderator_name), moderator_affiliation: opt(s.moderator_affiliation) }))
      : kind === "organizers"
        ? people.map((p) => ({ id: p.id, name: p.name, kind: p.kind, role: opt(p.role), affiliation: opt(p.affiliation), job_title: opt(p.job_title), photo_url: opt(p.photo_url) }))
        : partners.map((p) => ({ id: p.id, name: p.name, logo_url: opt(p.logo_url), website_url: opt(p.website_url), tier: opt(p.tier) }));
    try {
      await request<void>(`/conferences/${conferenceId}/${kind}`, { method: "PUT", body: { data } });
      // Reload to pick up ids of new rows, so the next save updates them in place.
      const fresh = await request<ContentResponse>(`/my/conferences/${conferenceId}/content`);
      if (kind === "sections") setSections(toSections(fresh));
      if (kind === "organizers") setPeople(toPeople(fresh));
      if (kind === "partners") setPartners(toPartners(fresh));
      setMessage(tr("Изменения сохранены.", "Changes saved."));
      onSaved?.();
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }

  if (loaded !== conferenceId) return error ? <p className={page.error} role="alert">{error}</p> : <p className={page.notice}>{tr("Загружаем материалы мероприятия…", "Loading event content…")}</p>;

  const rowActions = <T,>(items: T[], set: (value: T[]) => void, index: number) => (
    <div className={styles.rowActions}>
      <Button type="button" variant="ghost" disabled={index === 0} onClick={() => set(move(items, index, -1))} aria-label={tr("Выше", "Move up")}>↑</Button>
      <Button type="button" variant="ghost" disabled={index === items.length - 1} onClick={() => set(move(items, index, 1))} aria-label={tr("Ниже", "Move down")}>↓</Button>
      <Button type="button" variant="ghost" onClick={() => set(items.filter((_, i) => i !== index))}>{tr("Удалить", "Remove")}</Button>
    </div>
  );

  return (
    <div className={styles.editor}>
      {error && <p className={page.error} role="alert">{error}</p>}
      {message && <p className={page.success} role="status">{message}</p>}

      <fieldset className={`${page.panel} ${styles.group}`}>
        <legend>{tr("Секции программы", "Programme sections")}</legend>
        <p className={styles.hint}>{tr("Доклады привязываются к секциям в форме сессии. Порядок здесь — порядок показа.", "Talks are assigned to sections in the session form. The order here is the display order.")}</p>
        {sections.map((s, i) => (
          <div key={s.id ?? `new-${i}`} className={styles.row}>
            <div className={page.row}>
              <label className={page.field}>{tr("Название", "Title")}<input required maxLength={500} value={s.title} onChange={(e) => setSections(sections.map((x, j) => j === i ? { ...x, title: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Название на английском", "Title in English")}<input maxLength={500} value={s.title_en} onChange={(e) => setSections(sections.map((x, j) => j === i ? { ...x, title_en: e.target.value } : x))} /></label>
            </div>
            <div className={page.row}>
              <label className={page.field}>{tr("Модератор", "Moderator")}<input maxLength={255} value={s.moderator_name} onChange={(e) => setSections(sections.map((x, j) => j === i ? { ...x, moderator_name: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Организация модератора", "Moderator affiliation")}<input maxLength={255} value={s.moderator_affiliation} onChange={(e) => setSections(sections.map((x, j) => j === i ? { ...x, moderator_affiliation: e.target.value } : x))} /></label>
            </div>
            <label className={page.field}>{tr("Описание", "Description")}<textarea maxLength={5000} value={s.description} onChange={(e) => setSections(sections.map((x, j) => j === i ? { ...x, description: e.target.value } : x))} /></label>
            {rowActions(sections, setSections, i)}
          </div>
        ))}
        <div className={page.inlineActions}>
          <Button type="button" variant="secondary" onClick={() => setSections([...sections, { ...emptySection }])}>{tr("Добавить секцию", "Add section")}</Button>
          <Button type="button" disabled={busy !== ""} onClick={() => save("sections")}>{busy === "sections" ? tr("Сохраняем…", "Saving…") : tr("Сохранить секции", "Save sections")}</Button>
        </div>
      </fieldset>

      <fieldset className={`${page.panel} ${styles.group}`}>
        <legend>{tr("Оргкомитет и программный комитет", "Organizing and programme committees")}</legend>
        {people.map((p, i) => (
          <div key={p.id ?? `new-${i}`} className={styles.row}>
            <div className={page.row}>
              <label className={page.field}>{tr("Имя", "Name")}<input required maxLength={255} value={p.name} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Комитет", "Committee")}<select value={p.kind} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, kind: e.target.value as Person["kind"] } : x))}><option value="organizer">{tr("Организационный", "Organizing")}</option><option value="committee">{tr("Программный", "Programme")}</option></select></label>
            </div>
            <div className={page.row}>
              <label className={page.field}>{tr("Организация", "Affiliation")}<input maxLength={255} value={p.affiliation} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, affiliation: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Должность", "Position")}<input maxLength={255} value={p.job_title} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, job_title: e.target.value } : x))} /></label>
            </div>
            <div className={page.row}>
              <label className={page.field}>{tr("Роль в комитете", "Committee role")}<input maxLength={100} placeholder={tr("например, председатель", "e.g. chair")} value={p.role} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, role: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Фото (HTTPS)", "Photo (HTTPS)")}<input type="url" maxLength={1000} value={p.photo_url} onChange={(e) => setPeople(people.map((x, j) => j === i ? { ...x, photo_url: e.target.value } : x))} /></label>
            </div>
            {rowActions(people, setPeople, i)}
          </div>
        ))}
        <div className={page.inlineActions}>
          <Button type="button" variant="secondary" onClick={() => setPeople([...people, { ...emptyPerson }])}>{tr("Добавить человека", "Add person")}</Button>
          <Button type="button" disabled={busy !== ""} onClick={() => save("organizers")}>{busy === "organizers" ? tr("Сохраняем…", "Saving…") : tr("Сохранить комитеты", "Save committees")}</Button>
        </div>
      </fieldset>

      <fieldset className={`${page.panel} ${styles.group}`}>
        <legend>{tr("Партнёры", "Partners")}</legend>
        {partners.map((p, i) => (
          <div key={p.id ?? `new-${i}`} className={styles.row}>
            <div className={page.row}>
              <label className={page.field}>{tr("Название", "Name")}<input required maxLength={255} value={p.name} onChange={(e) => setPartners(partners.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Статус партнёра", "Partner tier")}<input maxLength={50} placeholder={tr("например, генеральный", "e.g. general")} value={p.tier} onChange={(e) => setPartners(partners.map((x, j) => j === i ? { ...x, tier: e.target.value } : x))} /></label>
            </div>
            <div className={page.row}>
              <label className={page.field}>{tr("Сайт (HTTPS)", "Website (HTTPS)")}<input type="url" maxLength={1000} value={p.website_url} onChange={(e) => setPartners(partners.map((x, j) => j === i ? { ...x, website_url: e.target.value } : x))} /></label>
              <label className={page.field}>{tr("Логотип (HTTPS)", "Logo (HTTPS)")}<input type="url" maxLength={1000} value={p.logo_url} onChange={(e) => setPartners(partners.map((x, j) => j === i ? { ...x, logo_url: e.target.value } : x))} /></label>
            </div>
            {rowActions(partners, setPartners, i)}
          </div>
        ))}
        <div className={page.inlineActions}>
          <Button type="button" variant="secondary" onClick={() => setPartners([...partners, { ...emptyPartner }])}>{tr("Добавить партнёра", "Add partner")}</Button>
          <Button type="button" disabled={busy !== ""} onClick={() => save("partners")}>{busy === "partners" ? tr("Сохраняем…", "Saving…") : tr("Сохранить партнёров", "Save partners")}</Button>
        </div>
      </fieldset>
    </div>
  );
}
