import type { CoAuthor } from "../../types/conference";
import { Button } from "../ui/Button/Button";
import { tr } from "../../i18n/runtime";
import page from "../../pages/Page.module.css";
import { MAX_CO_AUTHORS } from "./coAuthors";

interface CoAuthorsEditorProps {
  value: CoAuthor[];
  onChange: (value: CoAuthor[]) => void;
  disabled?: boolean;
}

/** Editable list of co-authors: name is required, email and affiliation are optional. */
export function CoAuthorsEditor({ value, onChange, disabled }: CoAuthorsEditorProps) {
  const update = (index: number, patch: Partial<CoAuthor>) =>
    onChange(value.map((item, i) => (i === index ? { ...item, ...patch } : item)));
  return (
    <fieldset className={page.form} disabled={disabled}>
      <legend>{tr("Соавторы", "Co-authors")}</legend>
      {value.length === 0 && <p className={page.notice}>{tr("Соавторов нет.", "No co-authors.")}</p>}
      {value.map((item, index) => (
        <div key={index} className={page.row}>
          <label className={page.field}>{tr("Имя и фамилия", "Full name")}<input required maxLength={255} value={item.name} onChange={(event) => update(index, { name: event.target.value })} /></label>
          <label className={page.field}>{tr("Email", "Email")}<input type="email" maxLength={255} value={item.email ?? ""} onChange={(event) => update(index, { email: event.target.value })} /></label>
          <label className={page.field}>{tr("Организация", "Affiliation")}<input maxLength={255} value={item.affiliation ?? ""} onChange={(event) => update(index, { affiliation: event.target.value })} /></label>
          <Button type="button" variant="ghost" onClick={() => onChange(value.filter((_, i) => i !== index))}>{tr("Удалить", "Remove")}</Button>
        </div>
      ))}
      {value.length < MAX_CO_AUTHORS && (
        <Button type="button" variant="secondary" onClick={() => onChange([...value, { name: "" }])}>{tr("Добавить соавтора", "Add co-author")}</Button>
      )}
    </fieldset>
  );
}
