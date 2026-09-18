import type { Session } from "./api";
import type { ConfigurationSelection } from "./projectApi";
import type { OperationIdentity } from "./projectRecovery";
import type { RepositorySelection } from "./projectDrafts";
import type { SessionController } from "./session";

export type Navigate = (path: string) => void;
export type AuthenticatedPageProps = { session: Session; auth: SessionController; navigate: Navigate };
export type ProjectPageProps = AuthenticatedPageProps & { projectId: string };
export type ProjectRecoveryProps = AuthenticatedPageProps & { operation: OperationIdentity };
export type RepositoryPickerProps = AuthenticatedPageProps & { selection: RepositorySelection; onChange: (selection: RepositorySelection) => void; onDone: () => void; maximumSelection: number };
export type ConfigurationFieldsProps = AuthenticatedPageProps & { value: ConfigurationSelection; onChange: (value: ConfigurationSelection) => void; disabled: boolean };
