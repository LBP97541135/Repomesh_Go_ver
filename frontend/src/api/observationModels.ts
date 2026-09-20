import { apiRequest } from "./http";

export type ObservationPurpose = "jev" | "deepseek";
export interface ObservationModelConfig {
  model: string;
  configured: boolean;
  endpoint: string;
}
export interface ObservationModelTest {
  ok: boolean;
  authentication_ok: boolean;
  models: string[];
  selected_model_listing: string;
  message: string;
}
const path = (purpose: ObservationPurpose) => `/settings/observation-models/${purpose}`;
export const readObservationModel = (purpose: ObservationPurpose) => apiRequest<ObservationModelConfig>("GET", path(purpose));
export const saveObservationModel = (purpose: ObservationPurpose, model: string, apiKey: string) =>
  apiRequest<ObservationModelConfig>("POST", path(purpose), { model, api_key: apiKey });
export const testObservationModel = (purpose: ObservationPurpose) => apiRequest<ObservationModelTest>("POST", `${path(purpose)}/test`, {});
