import { useEffect, useRef, useState } from "react";
import SwaggerUI from "swagger-ui-dist/swagger-ui-bundle.js";
import "swagger-ui-dist/swagger-ui.css";
import { tr } from "../i18n/runtime";
import styles from "./Page.module.css";
import docsStyles from "./ApiDocsPage.module.css";

const services = ["identity", "conference", "application", "engagement"] as const;

export function ApiDocsPage() {
  const [service, setService] = useState<(typeof services)[number]>("identity");
  const container = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const node = container.current;
    if (!node) return;
    const controls = new MutationObserver(() => {
      node.querySelector("#servers")?.setAttribute("aria-label", tr("Сервер API", "API server"));
      node.querySelector("main")?.setAttribute("role", "presentation");
      node.querySelectorAll(".highlight-code pre").forEach((example) => {
        example.setAttribute("tabindex", "0");
        example.setAttribute("aria-label", tr("Пример запроса или ответа", "Request or response example"));
      });
    });
    controls.observe(node, { childList: true, subtree: true });
    SwaggerUI({
      domNode: node,
      url: `/api/v1/openapi/${service}.json?scope=public`,
      validatorUrl: null,
      supportedSubmitMethods: [],
      persistAuthorization: false,
      docExpansion: "none",
      filter: true,
    });
    return () => { controls.disconnect(); node.replaceChildren(); };
  }, [service]);

  return (
    <section className={`${styles.panel} ${docsStyles.docs}`}>
      <h1>{tr("Документация API", "API documentation")}</h1>
      <p>{tr("Методы API для работы с аккаунтом, мероприятиями, заявками и уведомлениями. Выберите сервис для просмотра запросов и ответов.", "API operations for accounts, events, applications and notifications. Select a service to explore requests and responses.")}</p>
      <label className={styles.field}>
        {tr("Сервис", "Service")}
        <select value={service} onChange={(event) => setService(event.target.value as (typeof services)[number])}>
          {services.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
      </label>
      <a href={`/api/v1/openapi/${service}.json?scope=public`} target="_blank" rel="noreferrer">OpenAPI JSON</a>
      <div ref={container} className={docsStyles.viewer} />
    </section>
  );
}
