import { useEffect, useState } from "react";
import { Hero } from "../components/home/Hero/Hero";
import { ConferenceCatalog } from "../components/home/ConferenceCatalog/ConferenceCatalog";
import { useAuth } from "../auth/useAuth";
import { readableError } from "../api/client";
import type { HomeResponse } from "../types/conference";
import styles from "./Page.module.css";

export function HomePage() {
  const { request } = useAuth();
  const [home, setHome] = useState<HomeResponse | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    request<HomeResponse>("/home?stats=counts", { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setHome(result); })
      .catch((reason: unknown) => { if (!controller.signal.aborted) setError(readableError(reason)); });
    return () => controller.abort();
  }, [request]);

  return (
    <>
      {error && <p className={styles.error} role="alert">{error}</p>}
      <Hero data={home} />
      <ConferenceCatalog />
    </>
  );
}
