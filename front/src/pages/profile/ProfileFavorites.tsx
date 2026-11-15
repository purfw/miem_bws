import { useCallback } from "react";
import { useAuth } from "../../auth/useAuth";
import { ConferenceCard } from "../../components/home/ConferenceCard/ConferenceCard";
import type { Conference, PaginationData } from "../../types/conference";
import { tr } from "../../i18n/runtime";
import { useProfileResource } from "./useProfileResource";
import { ProfileEmpty, ProfileLoadError, ProfileSkeleton } from "./ProfileUI";
import styles from "./Profile.module.css";

export function ProfileFavorites() {
  const { request } = useAuth();
  const load = useCallback(
    async (signal: AbortSignal) => {
      const items: Conference[] = [];
      let page = 1;
      let totalPages: number;
      do {
        const result = await request<{
          data: Conference[];
          pagination: PaginationData;
        }>(`/favorites?per_page=100&page=${page}`, { signal });
        items.push(...result.data);
        totalPages = result.pagination.total_pages;
        page++;
      } while (page <= totalPages);
      return items;
    },
    [request],
  );
  const resource = useProfileResource(load);
  if (resource.loading) return <ProfileSkeleton />;
  if (resource.error)
    return <ProfileLoadError error={resource.error} retry={resource.retry} />;
  if (!resource.data?.length)
    return (
      <ProfileEmpty>
        {tr(
          "Сохраняйте интересные мероприятия — они появятся здесь.",
          "Save events you are interested in to find them here.",
        )}
      </ProfileEmpty>
    );
  return (
    <div className={styles.cards}>
      {resource.data.map((conference) => (
        <ConferenceCard key={conference.id} conference={conference} />
      ))}
    </div>
  );
}
