# k8s-cronjob-manager

A small web UI and API to run, pause and inspect Kubernetes CronJobs without handing out kubectl access.

쿠버네티스 CronJob을 kubectl 권한 없이 안전하게 운영하기 위한 작은 웹 도구다. 목록 확인, 지금 실행, 일시정지와 재개, 실행 이력, Pod 로그 확인까지 한 화면에서 한다.

## 문제

VM의 크론탭으로 돌던 배치를 쿠버네티스 CronJob으로 옮긴 뒤 운영에서 반복되는 요청이 생겼다.

- "지금 한 번만 돌려주세요" (데이터 보정, 장애 후 재처리)
- "점검 동안 잠깐 멈춰주세요"
- "어젯밤 배치 성공했나요? 로그 좀 봐주세요"

매번 kubectl로 대신 처리하거나, 요청한 사람에게 클러스터 권한을 줘야 했다. 둘 다 마음에 들지 않았다. 앞의 방법은 사람이 병목이 되고, 뒤의 방법은 필요 이상으로 권한이 넓어진다.

그래서 필요한 동작 다섯 가지만 열어 주는 도구를 만들었다. CronJob을 만들거나 고치는 일은 지금처럼 Git과 배포 파이프라인에서 하고, 이 도구는 실행과 확인만 맡는다.

## 기능

| 기능 | 동작 |
|---|---|
| 목록 | 허용된 네임스페이스의 CronJob. 스케줄, 타임존, 다음 실행 시각, 마지막 성공 시각, 실행 중 Job 수 |
| 상세 | 동시 실행 정책, 보관 이력 수, backoffLimit, 컨테이너 이미지, 라벨 |
| 지금 실행 | `kubectl create job --from=cronjob/<name>`과 같은 방식으로 jobTemplate에서 Job 생성 |
| 일시정지 / 재개 | `spec.suspend`만 merge patch |
| 실행 이력 | CronJob이 소유한(ownerReference) Job, 최신순. 수동 실행 표시 |
| 로그 | Job의 Pod와 컨테이너 선택, 마지막 N줄, 크기 상한, 5초 자동 새로고침, 다운로드 |

지금 실행은 kubectl과 같은 규칙을 따른다.

- 이름은 `<cronjob>-manual-<unix초>`이고, 63자를 넘으면 CronJob 이름 쪽을 자른다. Job 이름이 Pod 라벨 값(최대 63자)으로 들어가기 때문이다.
- `cronjob.kubernetes.io/instantiate: manual` 어노테이션과 CronJob을 가리키는 controller ownerReference를 붙인다. 그래서 이력에 같이 잡히고, CronJob을 지우면 함께 정리된다.
- `concurrencyPolicy: Forbid`인 CronJob은 아직 끝나지 않은 Job이 있으면 수동 실행을 409로 거절한다. kubectl은 이 검사를 하지 않는다.

## 구조

```
.
├── cmd/cronjob-manager/   # main: 설정 로드, client-go 연결, HTTP 서버, graceful shutdown
├── internal/
│   ├── config/            # 환경 변수 파싱과 검증
│   ├── manager/           # 쿠버네티스 로직 (목록, 실행, 일시정지, 이력, 로그)
│   └── api/               # net/http 라우팅, 토큰 확인, 오류 코드 매핑, UI 서빙
├── web/                   # React + TypeScript + Vite UI, embed.go
│   └── dist/placeholder/  # UI를 빌드하지 않았을 때 보이는 안내 페이지 (커밋됨)
├── deploy/                # Namespace, ServiceAccount, RBAC, Deployment, Service
│   └── example/           # kind에서 써볼 예제 CronJob
├── Dockerfile             # node 빌드 > go 빌드 > distroless nonroot
└── Makefile
```

- 백엔드는 Go 표준 라이브러리 `net/http`(1.22+ 메서드/경로 패턴)와 client-go만 쓴다. 로그는 `log/slog` JSON이다.
- UI 빌드 결과(`web/dist/ui`)는 `go:embed`로 바이너리에 들어간다. 바이너리 하나, 컨테이너 하나로 끝난다.
- 새로 clone한 상태에서도 `go build ./... && go test ./...`가 Node 없이 통과한다. 이때는 UI 대신 안내 페이지가 나오고 API는 그대로 동작한다.
- 테스트는 `k8s.io/client-go/kubernetes/fake`로 목록, 실행, 일시정지, 이력, 로그를 확인하고, `httptest`로 핸들러의 상태 코드와 토큰, 교차 출처 차단을 확인한다.

## 실행

### 1. 로컬에서 kubeconfig로 (kind)

```bash
kind create cluster
kubectl apply -f deploy/example/batch-jobs.yaml

# API만 (UI는 안내 페이지)
NAMESPACES=batch-jobs go run ./cmd/cronjob-manager

# UI 포함
make build
NAMESPACES=batch-jobs ./bin/cronjob-manager
# http://localhost:8080
```

현재 kubeconfig 컨텍스트를 쓴다. 다른 컨텍스트를 쓰려면 `KUBE_CONTEXT`를 지정한다. 회사 클러스터 컨텍스트가 기본으로 잡혀 있지 않은지 먼저 확인하는 편이 좋다.

UI를 고칠 때는 백엔드를 띄워 둔 채로 `cd web && npm run dev`를 실행한다. Vite 개발 서버가 `/api`를 8080으로 넘긴다.

### 2. 클러스터 안에서 (in-cluster)

```bash
make kind-load                          # 이미지 빌드 후 kind에 적재
kubectl apply -k deploy/

# 선택: 토큰 사용
kubectl -n cronjob-manager create secret generic cronjob-manager \
  --from-literal=auth-token="$(openssl rand -hex 24)"
kubectl -n cronjob-manager rollout restart deploy/cronjob-manager

kubectl -n cronjob-manager port-forward svc/cronjob-manager 8080:80
# http://localhost:8080
```

다른 클러스터에서는 이미지를 레지스트리에 올리고 `deploy/deployment.yaml`의 `image`를 바꾼다.

### 환경 변수

| 이름 | 기본값 | 설명 |
|---|---|---|
| `NAMESPACES` | (필수) | 허용할 네임스페이스, 쉼표로 구분 |
| `LABEL_SELECTOR` | 없음 | 이 셀렉터에 맞는 CronJob만 보인다. 예: `team=data` |
| `READ_ONLY` | `false` | `true`면 실행, 일시정지, 재개를 막는다 |
| `AUTH_TOKEN` | 없음 | 설정하면 모든 `/api/` 요청에 `Authorization: Bearer <token>` 필요. 16자 이상 |
| `LISTEN_ADDR` | `:8080` | |
| `LOG_TAIL_LINES` | `500` | 로그 기본 줄 수. 요청당 최대 5000 |
| `LOG_LIMIT_BYTES` | `1048576` | 로그 응답 크기 상한 |
| `KUBECONFIG`, `KUBE_CONTEXT` | 없음 | 클러스터 밖에서 실행할 때만 사용 |
| `LOG_LEVEL` | `info` | `debug`면 요청마다 접근 로그를 남긴다 |

## API

모든 응답은 JSON이다. 오류는 `{"error": "..."}` 형태다.

| 메서드 | 경로 | 설명 |
|---|---|---|
| GET | `/healthz` | 상태 확인 (토큰 불필요) |
| GET | `/api/v1/config` | 허용 네임스페이스, 읽기 전용 여부, 라벨 셀렉터 |
| GET | `/api/v1/cronjobs?namespace=` | CronJob 목록. namespace를 빼면 허용된 전체 |
| GET | `/api/v1/jobs?limit=` | 모든 관리 대상 CronJob의 최근 Job |
| GET | `/api/v1/namespaces/{ns}/cronjobs/{name}` | CronJob 상세 |
| POST | `/api/v1/namespaces/{ns}/cronjobs/{name}/run` | 지금 실행. 201과 생성된 Job |
| POST | `/api/v1/namespaces/{ns}/cronjobs/{name}/suspend` | 일시정지 |
| POST | `/api/v1/namespaces/{ns}/cronjobs/{name}/resume` | 재개 |
| GET | `/api/v1/namespaces/{ns}/cronjobs/{name}/jobs?limit=` | 실행 이력 (기본 20, 최대 100) |
| GET | `/api/v1/namespaces/{ns}/cronjobs/{name}/jobs/{job}/logs?pod=&container=&tailLines=` | Job 로그 |

| 상태 코드 | 의미 |
|---|---|
| 401 | 토큰이 없거나 틀림 |
| 403 | 허용 목록 밖의 네임스페이스, 읽기 전용 모드, 교차 출처 POST |
| 404 | 없음, 또는 라벨 셀렉터 밖이라 보이지 않음 |
| 409 | Forbid 정책인데 실행 중인 Job이 있음, 같은 초에 같은 이름으로 다시 실행 |
| 400 | 잘못된 쿼리 값, Job에 속하지 않은 Pod나 컨테이너 |

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  localhost:8080/api/v1/namespaces/batch-jobs/cronjobs/hello/run
```

## 보안 모델

권한을 세 겹으로 좁힌다. 앞의 것이 뚫려도 뒤의 것이 막도록 했다.

1. **쿠버네티스 RBAC**: `deploy/clusterrole.yaml`에 필요한 동사만 넣었다.

   | 리소스 | 동사 | 이유 |
   |---|---|---|
   | `batch/cronjobs` | get, list, watch, patch | 조회, `spec.suspend` 변경 |
   | `batch/jobs` | get, list, create | 이력, 지금 실행 |
   | `pods` | get, list | Job의 Pod 찾기 |
   | `pods/log` | get | 로그 |

   delete, update, Secret, ConfigMap, exec 권한은 없다. ClusterRole로 정의하지만 ClusterRoleBinding은 쓰지 않는다. 허용할 네임스페이스마다 RoleBinding을 하나씩 만들어 그 네임스페이스에서만 권한이 생기게 한다. 읽기 전용으로 운영할 때는 `cronjob-manager-viewer`를 바인딩하고 `READ_ONLY=true`를 함께 켠다. 앱 설정을 실수로 바꿔도 API 서버가 쓰기를 거절한다.

2. **앱 안의 제한**
   - 네임스페이스 허용 목록 밖의 요청은 쿠버네티스를 호출하기 전에 403으로 끝낸다.
   - 라벨 셀렉터 밖의 CronJob은 404로 답해서 존재 여부도 드러내지 않는다.
   - 로그는 "이 CronJob이 소유한 Job의 Pod"만 읽는다. 허용된 네임스페이스라도 다른 Pod의 로그는 볼 수 없다.
   - 로그는 줄 수와 바이트 상한이 있다.
   - 실행, 일시정지, 재개는 감사 로그(`audit:` 접두어, 네임스페이스, 이름, Job 이름, 요청 주소)를 남긴다.

3. **접근 경로**
   - 이 도구는 사용자 로그인을 직접 구현하지 않는다. 다음 중 하나로 앞을 막는 것을 전제로 한다.
     - `kubectl port-forward`: 클러스터 접근 권한이 있는 사람만 쓴다.
     - [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) 같은 인증 프록시 뒤에 두고 사내 SSO 그룹으로 제한한다.
   - `AUTH_TOKEN`은 그 위에 얹는 단순한 공유 비밀이다. 토큰만으로 인터넷에 노출하는 용도가 아니다.
   - 쿠키 기반 프록시 뒤에서도 다른 사이트가 사용자 브라우저로 POST를 보내지 못하도록 Go의 `http.CrossOriginProtection`을 적용했다.
   - 컨테이너는 distroless nonroot 이미지, `readOnlyRootFilesystem`, 모든 capability 제거, `RuntimeDefault` seccomp로 돈다. 네임스페이스에는 Pod Security `restricted`를 걸었다.

## 한계

- 이력은 클러스터에 남아 있는 Job만 보여 준다. `successfulJobsHistoryLimit`, `failedJobsHistoryLimit`, `ttlSecondsAfterFinished`로 지워진 실행은 나오지 않는다. 오래 보관하려면 로그 수집기나 이벤트 저장소가 따로 필요하다.
- 이력을 만들 때 네임스페이스의 Job을 모두 list한 뒤 ownerReference로 거른다. informer 캐시를 쓰지 않아서 Job이 아주 많은 네임스페이스에서는 느릴 수 있다.
- 사용자 구분이 없다. 토큰이나 프록시를 통과하면 모두 같은 권한이다. 감사 로그에 사람 이름이 남으려면 프록시가 넘겨주는 사용자 헤더를 기록하도록 고쳐야 한다.
- 로그는 스트리밍이 아니라 5초 간격 새로고침이다. 크기 상한을 넘으면 앞부분만 보인다.
- `spec.timeZone`이 없는 CronJob의 다음 실행 시각은 UTC로 계산한다. 실제로는 kube-controller-manager의 타임존을 따르므로 다를 수 있다.
- 지금 실행은 jobTemplate을 그대로 쓴다. 인자나 환경 변수를 바꿔서 실행하는 기능은 일부러 넣지 않았다. 바꿔야 하면 Git에서 바꾸는 것이 맞다고 봤다.
- CronJob 생성, 수정, 삭제는 범위 밖이다.

## 개발

```bash
go vet ./... && go test -race ./...
cd web && npm ci && npm run build
```

과정과 배경은 블로그에 정리하고 있다: https://thereisnotherway.tistory.com/

## License

MIT
