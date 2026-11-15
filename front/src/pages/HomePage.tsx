import { useEffect, useState } from "react";
import { Hero } from "../components/home/Hero/Hero";
import { ConferenceCatalog } from "../components/home/ConferenceCatalog/ConferenceCatalog";
import { useAuth } from "../auth/useAuth";
import type { HomeResponse } from "../types/conference";

export function HomePage() {
  const { request } = useAuth();
  const [home, setHome] = useState<HomeResponse | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    request<HomeResponse>("/home?stats=counts", { signal: controller.signal }).then(setHome).catch(() => undefined);
    return () => controller.abort();
  }, [request]);

  return (
    <>
      <Hero data={home} />
      <ConferenceCatalog />
    </>
  );
}
