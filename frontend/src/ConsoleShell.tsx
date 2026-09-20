import { useCallback, useEffect, useRef, useState } from "react";
import { Toast } from "./components/Toast";
import { AppInstallGuide } from "./components/AppInstallGuide";
import { AuthError, authApi, type Account } from "./api/auth";
import { LoginPage } from "./components/LoginPage";
import { SidebarV2, type NavKey } from "./components/SidebarV2";
import { CommandPalette } from "./components/CommandPalette";
import type { IssueListItemView, IssueListResponse } from "./api/contract";
import { archiveIssue, createIssue, fetchIssues, issuesSourceMode, purgeIssue, type CreateIssueRequest } from "./api/issues";
import { errText, shortId } from "./display";
import type { HumanReviewRequestView } from "./api/reviewDesk";
import { fetchReviewRequests, subscribeReviewRequests } from "./api/reviewDesk";
import { DecisionChainPage } from "./pages/DecisionChainPage";
import { IssueListPage } from "./pages/IssueListPage";
import { ObserveHome } from "./pages/observe/ObserveHome";
import { RepositoriesPage } from "./pages/RepositoriesPage";
import { RepositoryTeamPage } from "./pages/RepositoryTeamPage";
import { ProjectSelectPage } from "./pages/ProjectSelectPage";
import { beginProjectSession, clearActiveProject, setActiveProject } from "./api/activeProject";
import { listAllProjects, type ProjectListItem } from "./api/projects";
import { ReviewDeskPage } from "./pages/ReviewDeskPage";
import { RoomViewContainer } from "./pages/RoomViewContainer";
import { SettingsPage } from "./pages/SettingsPage";
import { SetupWizardPage } from "./pages/SetupWizardPage";
import { fetchSetupStatus } from "./api/platformSetup";
import { WorkbenchPage } from "./pages/workbench/WorkbenchPage";
import { NAV_HASH, parseTeamRepositoryId, readRoute, type Route } from "./routes";

/** v2 控制台外壳：身份门 → 侧栏导航 → 主区页面。
 *  路由用 hash（#/issues 等），不引入路由库。
 *
 *  **登录门于 2026-08-14 恢复**（裁决推翻 08-12 的「无登录门」）。当初拆门的理由
 *  在当时成立：数据面全走动作 token，那道 `/auth/me` 门只是 UX 层的。这次把它装
 *  回来是因为理由不再成立——main 合并后进来的**建团**与**人工审核台**落在
 *  `human_control` 面上，那一面认的是本地账号会话（建团还要 `is_admin`），共享
 *  动作 token 换不到。控制台从此持两套凭据：数据面仍走动作 token，human_control
 *  面走这道门发的 cookie 会话（见 `api/auth.ts` 顶部为什么不能混用）。
 *
 *  四态与拆门前一致：checking（不闪主界面）/ unreachable（身份服务打不通，可见失败
 *  态而非静默）/ anonymous（登录页）/ authenticated。 */

export default function ConsoleShell() {
  const [account, setAccount] = useState<Account | null>(null);
  const [authState, setAuthState] = useState<"checking" | "anonymous" | "authenticated" | "unreachable">(
    "checking",
  );
  const [authNote, setAuthNote] = useState<string | null>(null);
  const [setupReady, setSetupReady] = useState<boolean | null>(null);
  const [setupRequested, setSetupRequested] = useState(false);
  const [projects, setProjects] = useState<ProjectListItem[] | null>(null);
  const [projectsError, setProjectsError] = useState<string | null>(null);
  const [projectsReload, setProjectsReload] = useState(0);
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null);
  const selectionRef = useRef<string | null>(null);
  const projectListEpoch = useRef(0);
  useEffect(() => {
    if (!account) return;
    const epoch = ++projectListEpoch.current;
    const saved = beginProjectSession(account.id);
    selectionRef.current = null;
    setActiveProjectId(null); setProjects(null);
    let cancelled = false;
    setProjectsError(null);
    listAllProjects().then(items => {
      if (cancelled || epoch !== projectListEpoch.current) return;
      setProjects(items);
      const id = saved && items.some(p => p.id === saved) ? saved : null;
      if (id) setActiveProject(id); else clearActiveProject();
      selectionRef.current = id;
      setActiveProjectId(id);
    }).catch(e => { if (!cancelled) setProjectsError(errText(e)); });
    return () => { cancelled = true; };
  }, [account, projectsReload]);

  const [route, setRoute] = useState<Route>(readRoute);
  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<number | undefined>(undefined);

  // ⌘K/Ctrl+K 命令面板：状态与全局快捷键在外壳（数据源同侧栏的 issues 轮询）。
  const [paletteOpen, setPaletteOpen] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // issue 列表：state 由服务端筛选（?state=），不做本地分 tab——分页下本地过滤
  // 等于拿部分结果冒充全量。工作区（organization_id）由前端持有，当前无组织读模型
  // （CONS-32）故不传 = 全部工作区。
  const [issueTab, setIssueTab] = useState<"open" | "closed">("open");
  // v0.5：已归档开关走服务端过滤（include_archived），不做本地过滤——分页下本地
  // 过滤等于拿部分结果冒充全量，与 tab 的裁决同一条。
  const [showArchived, setShowArchived] = useState(false);
  const [issues, setIssues] = useState<IssueListResponse | null>(null);
  const [issuesLoading, setIssuesLoading] = useState(true);
  const [issuesMore, setIssuesMore] = useState(false);
  const [issuesError, setIssuesError] = useState<string | null>(null);
  const [issuesReload, setIssuesReload] = useState(0);

  // 审核待办（迁移 2）。SSE 推 pending 列表，侧栏徽标与页面共用这一份——
  // 两处各取一次会让徽标和列表在刷新间隙互相矛盾。
  const [reviews, setReviews] = useState<HumanReviewRequestView[] | null>(null);
  const [reviewsError, setReviewsError] = useState<string | null>(null);
  const [reviewsStreaming, setReviewsStreaming] = useState(true);
  const [reviewsReload, setReviewsReload] = useState(0);
  /** 列表代际号（A3）：主取数 effect 每次执行 +1，「加载更多」按代际丢弃过期响应 */
  const issuesEpoch = useRef(0);

  const showToast = useCallback((text: string) => {
    setToast(text);
    window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast(null), 2800);
  }, []);

  useEffect(() => {
    let cancelled = false;
    authApi
      .me()
      .then((acc) => {
        if (cancelled) return;
        setAccount(acc);
        setAuthState("authenticated");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        // 401 = 未登录（正常）；0/5xx = 身份服务不可达（可见失败态，不静默）
        if (err instanceof AuthError && err.status === 401) setAuthState("anonymous");
        else {
          setAuthNote(errText(err));
          setAuthState("unreachable");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (authState !== "authenticated") return;
    let cancelled = false;
    fetchSetupStatus()
      .then((status) => !cancelled && setSetupReady(status.ready_for_project_creation))
      .catch(() => !cancelled && setSetupReady(false));
    return () => {
      cancelled = true;
    };
  }, [authState]);

  useEffect(() => {
    const onHash = () => setRoute(readRoute());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  useEffect(() => {
    // Both initial and paginated responses belong to this project/filter generation.
    const epoch = ++issuesEpoch.current;
    setIssues(null);
    setIssuesMore(false);
    if (authState !== "authenticated" || !activeProjectId) {
      setIssuesLoading(false);
      return;
    }
    let cancelled = false;
    setIssuesLoading(true);
    setIssuesError(null);
    fetchIssues({
      projectId: activeProjectId,
      state: issueTab,
      includeArchived: showArchived,
    })
      .then((page) => {
        if (cancelled || epoch !== issuesEpoch.current) return;
        setIssues(page);
        setIssuesLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled || epoch !== issuesEpoch.current) return;
        setIssuesError(errText(err));
        setIssuesLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [authState, activeProjectId, issueTab, issuesReload, showArchived]);

  useEffect(() => {
    if (authState !== "authenticated") return;
    let cancelled = false;
    setReviewsError(null);
    // 先一次性取一份垫底：SSE 的首帧要等到 store 有变化或首轮循环，
    // 空手等它会让首屏在两秒里说不清是「没有待办」还是「还没取到」。
    fetchReviewRequests("pending")
      .then((rows) => !cancelled && setReviews(rows))
      .catch((err: unknown) => !cancelled && setReviewsError(errText(err)));
    const unsubscribe = subscribeReviewRequests(
      (rows) => {
        if (cancelled) return;
        setReviews(rows);
        setReviewsStreaming(true);
        setReviewsError(null);
      },
      () => {
        // 流断不清空列表：最后一份结果仍然有用，页面会说明它不再更新。
        if (!cancelled) setReviewsStreaming(false);
      },
    );
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, [authState, reviewsReload]);

  const loadMoreIssues = () => {
    const cursor = issues?.next_cursor;
    if (!cursor || issuesMore || !activeProjectId) return;
    const epoch = issuesEpoch.current;
    setIssuesMore(true);
    fetchIssues({
      projectId: activeProjectId,
      state: issueTab,
      cursor,
      includeArchived: showArchived,
    })
      .then((page) => {
        if (epoch !== issuesEpoch.current) return; // A3：已切 tab/工作区，丢弃
        // 续读只追加条目；计数是全量值，以最新一页为准即可
        setIssues((prev) => (prev ? { ...page, issues: [...prev.issues, ...page.issues] } : page));
      })
      .catch((err: unknown) => { if (epoch === issuesEpoch.current) showToast(`加载更多失败：${errText(err)}`); })
      .finally(() => { if (epoch === issuesEpoch.current) setIssuesMore(false); });
  };

  /** 仓库作用域团队页：hash 里带仓库 id 时才渲染团队页（2026-09-20）。 */
  const teamRepositoryId = parseTeamRepositoryId(window.location.hash);

  const navigate = (nav: NavKey) => {
    window.location.hash = NAV_HASH[nav];
    setRoute({ nav, issueId: null, roomId: null, observeSection: null, settingsSection: null });
  };

  /** 跳到某个 issue。**能带上它所属项目就要带上** —— 人工审核台是跨项目的待办队列，
   *  而工作台取详情走的是 `GET /issues/{id}?projectId=<当前项目>`：项目不对就是 404。
   *  2026-09-20 线上实测：在审核台点「去 issue 处理」跳过去只有 404，就是因为这里
   *  只改路由、不切项目。 */
  const openIssue = (issueId: string, projectId?: string) => {
    if (projectId && projectId !== activeProjectId) {
      // 换项目的状态更新与 handleSelectProject 同一套（epoch 让在途的旧请求作废），
      // 只是不导航到列表 —— 路由由下面两句直接设成这个 issue。
      issuesEpoch.current += 1;
      setIssues(null);
      selectionRef.current = projectId;
      setActiveProject(projectId);
      setActiveProjectId(projectId);
    }
    window.location.hash = `#/issues/${issueId}`;
    setRoute({ nav: "issues", issueId, roomId: null, observeSection: null, settingsSection: null });
  };

  const handleSelectProject = (projectId: string, destination: "repositories" | "issues" = "issues") => {
    issuesEpoch.current += 1;
    setIssues(null);
    selectionRef.current = projectId;
    setActiveProject(projectId);
    setActiveProjectId(projectId);
    navigate(destination);
    // Includes freshly created projects; the side bar and manager share one list.
    const epoch = ++projectListEpoch.current;
    listAllProjects().then(items => { if (epoch === projectListEpoch.current) setProjects(items); }).catch(e => { if (epoch === projectListEpoch.current) setProjectsError(errText(e)); });
  };

  /** 新建 issue = 主页对话框：#/issues/new 就是「空流 + 可用输入框」的新会话态，
   *  发送即建 issue 并进入其对话视图（原 NewIssueModal 弹窗已按用户裁决退役）。 */
  const openNewSession = () => {
    window.location.hash = "#/issues/new";
    setRoute({ nav: "issues", issueId: "new", roomId: null, observeSection: null, settingsSection: null });
  };


  /** B-1 创建回路：POST /projects/{projectId}/issue-creations（B06 契约）→
   *  刷新列表 → 跳新 issue 详情。幂等键由弹窗/主页聊天框持有（A2：每次逻辑
   *  创建换键，重试沿用同键）。返回只承诺 issue_id，读模型字段由列表刷新提供。 */
  const handleCreateIssue = async (input: CreateIssueRequest, idempotencyKey: string) => {
    const issue = await createIssue(input, idempotencyKey);
    if (selectionRef.current === input.projectId) {
      showToast(`Issue 已创建：#${shortId(issue.issue_id)}`);
      setIssuesReload(n => n + 1);
      openIssue(issue.issue_id);
    }
    return issue;
  };

  /** v0.5 归档回路：POST /issues/{id}/archive（墓碑语义，不是删除）→ 刷新列表。
   *  replay 夹具不可篡改（createIssue 同一条红线），入口在页面层已藏、这里兜底。
   *  409（进行中）/其余失败：detail 原文上抛进 toast，不归并措辞。 */
  const handleArchiveIssue = async (item: IssueListItemView) => {
    if (issuesSourceMode() === "replay") {
      showToast("回放模式 · 归档不适用于夹具数据");
      return;
    }
    try {
      await archiveIssue(item.issue_id);
      showToast(`issue 已归档：#${shortId(item.issue_id)}（数据全部保留）`);
      setIssuesReload((n) => n + 1);
    } catch (err) {
      showToast(`归档失败：${errText(err)}`);
    }
  };

  /** 彻底清除回路（2026-09-08 用户裁决）：POST /issues/{id}/purge——不可逆的
   *  硬删除（快照/决策链/审计，仅留一条清除审计）。回放模式同上兜底拒绝。 */
  const handlePurgeIssue = async (item: IssueListItemView) => {
    if (issuesSourceMode() === "replay") {
      showToast("回放模式 · 彻底清除不适用于夹具数据");
      return;
    }
    try {
      const receipt = await purgeIssue(item.issue_id);
      showToast(
        `issue 已彻底清除：#${shortId(item.issue_id)}（快照 ${receipt.snapshots} · ` +
          `决策链 ${receipt.decision_chain_nodes} · 审计 ${receipt.audit_events}）`,
      );
      setIssuesReload((n) => n + 1);
    } catch (err) {
      showToast(`清除失败：${errText(err)}`);
    }
  };

  const handleLogout = () => {
    authApi
      .logout()
      .catch(() => undefined)
      .finally(() => {
        projectListEpoch.current += 1;
        clearActiveProject();
        selectionRef.current = null;
        setActiveProjectId(null);
        setProjects(null);
        setIssues(null);
        setAccount(null);
        setAuthState("anonymous");
      });
  };

  /** 切换账号：直接打 /api/auth/github/switch（不必先登出）。
   *  后端允许带活跃会话发起，回调成功后会作废本浏览器绑定上的旧会话再种新会话，
   *  所以这里只需整页跳转 GitHub 授权；失败（如 CSRF 过期）时回落到登录页。 */
  const handleSwitchAccount = () => {
    clearActiveProject();
    authApi.switchGithubAccount().catch((err: unknown) => {
      showToast(`切换账号失败：${errText(err)}`);
    });
  };

  if (authState === "checking") {
    return (
      <div className="grid h-screen place-items-center bg-ink">
        <p className="microlabel">校验会话…</p>
      </div>
    );
  }

  if (authState === "unreachable") {
    return (
      <div className="grid h-screen place-items-center bg-ink px-6">
        <div className="max-w-[520px] rounded-hard border border-salmon/60 bg-salmon/10 px-5 py-4">
          <div className="eyebrow mb-1.5 text-salmon">身份服务不可达</div>
          <p className="text-[12.5px] text-salmon">{authNote}</p>
          <p className="mt-2 text-[12px] text-tx2">
            控制平面需要本地身份服务（/api/v1/auth）。确认后端已启动后刷新页面。
          </p>
        </div>
      </div>
    );
  }

  if (authState === "anonymous" || !account) {
    return (
      <LoginPage
        onAuthenticated={(acc) => {
          setAccount(acc);
          setAuthState("authenticated");
        }}
      />
    );
  }

  if (setupReady === null) {
    return <div className="grid h-screen place-items-center bg-ink"><p className="microlabel">检查平台配置…</p></div>;
  }

  if ((!setupReady || setupRequested) && account.is_admin) {
    return (
      <SetupWizardPage
        account={account}
        onReady={() => {
          setSetupReady(true);
          setSetupRequested(false);
        }}
      />
    );
  }

  // 聊天工作台占主页新会话（无 hash/#/、#/issues/new）与每个 issue 的会话视图
  // （点列表里的 issue 进来就是每轮对话记录，用户 2026-09-05 裁决；旧详情页已删）。
  // 两者都是全高内滚布局；列表与房间页照旧带页边距。
  const isWorkbenchRoute = route.nav === "issues" && route.issueId !== null && route.roomId === null;

  return (
    <div className="flex h-screen overflow-hidden bg-ink text-tx">
      <SidebarV2
        account={account}
        nav={route.nav}
        issueCount={issues?.open_count ?? null}
        reviewCount={reviews?.length ?? null}
        onNavigate={navigate}
        onNewIssue={openNewSession}
        onLogout={handleLogout}
        onSwitchAccount={handleSwitchAccount}
        projects={projects}
        activeProjectId={activeProjectId}
        onSelectProject={handleSelectProject}
        onManageProjects={() => navigate("projects")}
        onOpenSearch={() => setPaletteOpen(true)}
      />

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        issues={issues?.issues ?? null}
        onNavigate={navigate}
        onOpenIssue={openIssue}
        onNewIssue={openNewSession}
      />

      {/* 工作台自带内滚与吸底输入框：容器不给页边距，交给页面自己（其余页面照旧） */}
      <main
        className={
          isWorkbenchRoute
            ? "flex min-w-0 flex-1 overflow-hidden"
            : "min-w-0 flex-1 overflow-y-auto px-8 pt-5 pb-10"
        }
      >
        {/* GitHub App 安装引导（2026-09-20）：在**动手之前**把「要装 App、点哪个链接」
            说清，而不是等人建 issue 时撞上 NO_AVAILABLE_REPOSITORIES 再回头猜。
            组件自己会在 uncoveredCount === 0、探测不可用之外的「已就绪」、
            或用户点过「稍后再说」时返回 null，所以这里无条件挂即可。
            唯一例外是工作台路由：那条路由的 main 是 flex 行（对话自带内滚与吸底输入框），
            塞一个块级卡片进去会横向挤压对话区。 */}
        {!isWorkbenchRoute && <AppInstallGuide variant="card" />}
        {route.nav === "issues" && activeProjectId !== null &&
          (route.issueId === null ? (
            <IssueListPage
              data={issues}
              tab={issueTab}
              loading={issuesLoading}
              loadingMore={issuesMore}
              error={issuesError}
              showArchived={showArchived}
              canArchive={issuesSourceMode() === "live"}
              onTab={setIssueTab}
              onToggleArchived={() => setShowArchived((v) => !v)}
              onArchive={handleArchiveIssue}
              onPurge={handlePurgeIssue}
              onLoadMore={loadMoreIssues}
              onRetry={() => setIssuesReload((n) => n + 1)}
              onOpenIssue={(item) => openIssue(item.issue_id)}
            />
          ) : route.issueId === "new" ? (
            <WorkbenchPage
              key={`${activeProjectId}:new`}
              projectId={activeProjectId}
              projectName={projects?.find(p => p.id === activeProjectId)?.name ?? activeProjectId}
              issueId={null}
              onCreateIssue={handleCreateIssue}
              onToast={showToast}
            />
          ) : route.roomId !== null ? (
            <RoomViewContainer
              key={`${activeProjectId}:${route.issueId}:${route.roomId}`}
              issueId={route.issueId}
              roomId={route.roomId}
              onBack={() => openIssue(route.issueId!)}
              onToast={showToast}
            />
          ) : (
            <WorkbenchPage
              key={`${activeProjectId}:${route.issueId}`}
              projectId={activeProjectId}
              projectName={projects?.find(p => p.id === activeProjectId)?.name ?? activeProjectId}
              issueId={route.issueId}
              onCreateIssue={handleCreateIssue}
              onBack={() => navigate("issues")}
              onToast={showToast}
            />
          ))}
        {route.nav === "reviews" && (
          <ReviewDeskPage
            rows={reviews}
            error={reviewsError}
            streaming={reviewsStreaming}
            onRefresh={() => setReviewsReload((n) => n + 1)}
            onToast={showToast}
            onOpenIssue={openIssue}
          />
        )}
      {route.nav === "repositories" && activeProjectId !== null && teamRepositoryId === null && (
        <RepositoriesPage
          key={activeProjectId}
          projectId={activeProjectId}
          projectName={projects?.find(p => p.id === activeProjectId)?.name ?? activeProjectId}
          onNewIssue={openNewSession}
          isAdmin={account.is_admin}
          onManageTeam={(repositoryId) => {
            window.location.hash = `#/repositories/${encodeURIComponent(repositoryId)}/team`;
          }}
        />
      )}
      {route.nav === "repositories" && teamRepositoryId !== null && (
        <RepositoryTeamPage
          key={teamRepositoryId}
          repositoryId={teamRepositoryId}
          isAdmin={account.is_admin}
          onBack={() => {
            window.location.hash = NAV_HASH.repositories;
          }}
          onToast={showToast}
        />
      )}
        {(route.nav === "projects" || (activeProjectId === null && ["issues", "repositories"].includes(route.nav))) && (
          <ProjectSelectPage key={account.id} projects={projects} activeProjectId={activeProjectId} error={projectsError} onRetry={() => setProjectsReload(n => n + 1)} onSelect={handleSelectProject} />
        )}
        {route.nav === "observe" && <ObserveHome section={route.observeSection} />}
        {route.nav === "decision-chains" && (
          <DecisionChainPage organizationId={null} onToast={showToast} />
        )}
        {route.nav === "settings" && (
          <SettingsPage
            key={route.settingsSection ?? "general"}
            account={account}
            onConfigure={() => setSetupRequested(true)}
            initialCategory={
              route.settingsSection === "local-cli"
                ? "localcli"
                : route.settingsSection === "models"
                  ? "models"
                  : route.settingsSection === "agents"
                    ? "agents"
                    : route.settingsSection === "skills"
                      ? "skills"
                      : "general"
            }
            onToast={showToast}
            onOpenIssue={openIssue}
          />
        )}
      </main>

      {toast && <Toast text={toast} />}
    </div>
  );
}
