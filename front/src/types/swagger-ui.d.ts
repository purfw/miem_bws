declare module "swagger-ui-dist/swagger-ui-bundle.js" {
  type Options = {
    domNode: HTMLElement;
    url: string;
    validatorUrl: null;
    supportedSubmitMethods: string[];
    persistAuthorization: boolean;
    docExpansion: string;
    filter: boolean;
  };
  export default function SwaggerUI(options: Options): unknown;
}
