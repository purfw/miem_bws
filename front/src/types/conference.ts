export type ConferenceStatus =
  | "draft"
  | "registration_open"
  | "registration_closed"
  | "in_progress"
  | "completed"
  | "cancelled"
  | "archived";

export type ConferenceFormat = "offline" | "hybrid" | "online";

export interface Venue {
  name: string;
  address: string;
  city?: string;
}

export interface Conference {
  id: string;
  slug: string;
  title: string;
  title_en?: string | null;
  description?: string | null;
  description_en?: string | null;
  format: ConferenceFormat;
  status: ConferenceStatus;
  registration_open: boolean;
  start_date: string;
  end_date: string;
  registration_deadline?: string | null;
  city?: string | null;
  primary_venue?: Venue | null;
  cover_image_url?: string | null;
  topics: string[];
  participants_count: number;
  max_participants?: number | null;
  sessions_count: number;
  is_favorite?: boolean;
  is_featured?: boolean;
  created_at: string;
}

export interface Organizer {
  id: string;
  name: string;
  affiliation?: string | null;
  job_title?: string | null;
  photo_url?: string | null;
  role: string;
  kind: "organizer" | "committee";
  sort_order: number;
}

export interface ConferenceSection {
  id: string;
  title: string;
  title_en?: string | null;
  description?: string | null;
  sort_order: number;
  talks_count: number;
}

export interface ScheduleSlot {
  id: string;
  section_id?: string | null;
  section_title?: string | null;
  title: string;
  speaker_name?: string | null;
  speaker_affiliation?: string | null;
  description?: string | null;
  room?: string | null;
  slot_date: string;
  start_time: string;
  end_time: string;
  session_type?: string;
}

export interface Partner {
  id: string;
  name: string;
  logo_url?: string | null;
  website_url?: string | null;
  tier?: string | null;
  sort_order: number;
}

export interface ConferenceDetail extends Conference {
  favorites_count: number;
  organizers: Organizer[];
  program_committee: Organizer[];
  sections: ConferenceSection[];
  schedule: ScheduleSlot[];
  partners: Partner[];
}

export interface PaginationData {
  page: number;
  per_page: number;
  total: number;
  total_pages: number;
}

export interface FilterOption {
  value: string;
  label: string;
  count: number;
  selected: boolean;
}

export interface CatalogFilter {
  title: string;
  key: string;
  type: "multi" | "radio" | "text" | "range";
  values?: FilterOption[];
  options?: string[];
  selected?: string;
  value?: string;
  from?: string;
  to?: string;
}

export interface ConferenceListResponse {
  data: Conference[];
  filters: CatalogFilter[];
  pagination: PaginationData;
}

export interface HomeResponse {
  heroConference?: Conference;
  heroInfo: {
    title: string;
    description: string;
    stats: { participants: number; conferences: number };
  };
}

export interface User {
  id: string;
  email: string;
  first_name: string;
  last_name: string;
  patronymic?: string | null;
  affiliation?: string | null;
  role: "user" | "verified_organizer" | "organizer_member" | "admin" | "student" | "moderator";
  email_verified: boolean;
  locale: "ru" | "en";
}

export interface Registration {
  id: string;
  conference_id: string;
  user_id: string;
  status: "confirmed" | "waitlisted" | "cancelled";
  role_at_conf: "attendee" | "speaker" | "reviewer";
  waitlist_position?: number;
  cancellation_count: number;
  registered_at: string;
  conference?: {
    id: string;
    slug: string;
    title: string;
    start_date: string;
    end_date: string;
    status: ConferenceStatus;
  };
}

export interface OrganizerVerification {
	role_applied: boolean;
  id: string;
  organization: string;
  method: "documents" | "trusted_domain" | "orcid";
  status: "pending" | "approved" | "rejected";
  rejection_reason?: string | null;
  created_at: string;
  reviewed_at?: string | null;
  documents: Array<{ id: string; name: string; mime_type: string; size_bytes: number; uploaded_at: string }>;
}

export interface ParticipationDocument {
  id: string;
  conference_id: string;
  conference_title: string;
  participant_name: string;
  issued_at: string;
}

export interface Subscription {
  id: string;
  conference_id: string | null;
  criteria?: { topics?: string[]; formats?: string[]; city?: string; from?: string; to?: string };
  notify_on: Array<"deadline_approaching" | "session_updated" | "speaker_added" | "conference_cancelled" | "conference_published">;
  channels: Array<"email">;
  created_at: string;
}

export interface Application {
  id: string;
  conference_id: string;
  conference_title: string;
  section_id?: string | null;
  user_id: string;
  form: "speaker" | "poster" | "listener";
  title: string;
  abstract?: string | null;
  keywords: string[];
  co_authors?: CoAuthor[];
  status: "submitted" | "under_review" | "revision_requested" | "accepted" | "rejected" | "withdrawn";
  created_at: string;
  updated_at?: string;
  moderator_comment?: string | null;
  files?: ApplicationFile[];
}

export interface CoAuthor {
  name: string;
  email?: string;
  affiliation?: string;
}

export interface ApplicationFile {
  id: string;
  original_name: string;
  mime_type: string;
  size_bytes: number;
  uploaded_at: string;
}

export interface ApplicationReview {
  id: string;
  reviewer_name: string;
  score: number;
  summary: string;
  strengths?: string | null;
  weaknesses?: string | null;
  recommendation: string;
}

export type RecommendationReasonCode =
  | "topic"
  | "similar_text"
  | "co_attendance"
  | "city"
  | "popular"
  | "registration_open";

export interface RecommendationReason {
  code: RecommendationReasonCode;
  value?: string;
}

export interface RecommendedConference extends Conference {
  reasons: RecommendationReason[];
}

export interface RecommendationsResponse {
  data: RecommendedConference[];
  personalised: boolean;
}
