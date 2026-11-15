export interface ProblemDetails {
  code?: string;
  title?: string;
  detail?: string;
  status?: number;
  errors?: Array<{ field?: string; message?: string }>;
}

export function passwordFieldError(error: unknown): string {
  if (!(error instanceof ApiError)) return "";
  if (error.problem?.code === "WEAK_PASSWORD") return tr("Этот пароль найден в утечках. Выберите другой пароль.", "This password has appeared in a data breach. Choose a different password.");
  return error.problem?.errors?.filter((item) => item.field === "password" || item.field === "new_password").map((item) => item.message).filter(Boolean).join(", ") || "";
}

export class ApiError extends Error {
  status: number;
  problem?: ProblemDetails;
  retryAfterSeconds?: number;

  constructor(status: number, problem?: ProblemDetails, retryAfterSeconds?: number) {
    const fieldMessage = problem?.errors?.map((item) => item.message).filter(Boolean).join(", ");
    super(fieldMessage || problem?.detail || problem?.title || `${tr("Ошибка сервера", "Server error")} (${status})`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

export interface ApiRequestOptions extends Omit<RequestInit, "body"> {
  body?: unknown;
  token?: string | null;
}

export async function apiRequest<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const { body, token, headers: suppliedHeaders, ...rest } = options;
  const headers = new Headers(suppliedHeaders);
  headers.set("Accept", "application/json");
  const isFormData = typeof FormData !== "undefined" && body instanceof FormData;
  if (body !== undefined && !isFormData) headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const response = await fetch(`/api/v1${path}`, {
    ...rest,
    headers,
    credentials: "include",
    body: body === undefined ? undefined : isFormData ? body : JSON.stringify(body),
  });

  if (!response.ok) {
    let problem: ProblemDetails | undefined;
    try {
      problem = (await response.json()) as ProblemDetails;
    } catch {
      problem = undefined;
    }
    const retryAfter = response.headers.get("Retry-After");
    const seconds = retryAfter ? (/^\d+$/.test(retryAfter) ? Number(retryAfter) : Math.ceil((Date.parse(retryAfter) - Date.now()) / 1000)) : undefined;
    throw new ApiError(response.status, problem, seconds && Number.isFinite(seconds) && seconds > 0 ? seconds : undefined);
  }

  if (response.status === 204) return undefined as T;
  const raw = await response.text();
  return (raw ? JSON.parse(raw) : undefined) as T;
}

export function readableError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.problem?.code === "EMAIL_NOT_VERIFIED") return tr("Для участия сначала подтвердите email в профиле.", "Please verify your email in your profile before registering.");
    if (error.problem?.code === "START_DATE_TOO_EARLY") return tr("Дата начала мероприятия должна быть не раньше 01.01.2027.", "The event start date must be on or after January 1, 2027.");
    if (error.problem?.code === "ALREADY_REGISTERED") return tr("Вы уже зарегистрированы на это мероприятие.", "You are already registered for this event.");
    if (error.problem?.code === "REGISTRATION_CLOSED") return tr("Регистрация на мероприятие закрыта или срок регистрации истёк.", "Event registration is closed or its deadline has passed.");
    if (error.problem?.code === "CANNOT_CANCEL") return tr("Участие уже отменено или мероприятие началось — отмена недоступна.", "Participation is already cancelled or the event has started; cancellation is unavailable.");
    if (error.problem?.code === "TOO_MANY_CANCELLATIONS") return tr("Достигнут лимит отмен участия в этом мероприятии.", "The cancellation limit for this event has been reached.");
    if (error.status === 401) return tr("Нужно войти в аккаунт.", "Please sign in.");
    if (error.status === 403) return tr("Недостаточно прав для этого действия.", "You do not have permission for this action.");
    if (error.status === 404) return tr("Запрошенные данные не найдены.", "The requested data was not found.");
    if (error.status === 429) return tr("Слишком много запросов. Попробуйте позднее.", "Too many requests. Please try again later.");
    return error.message;
  }
  if (error instanceof Error && error.message) return error.message;
  return tr("Не удалось связаться с сервером. Попробуйте ещё раз.", "Could not reach the server. Please try again.");
}
import { tr } from "../i18n/runtime";
