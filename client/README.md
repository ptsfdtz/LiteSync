# LiteSync Client

Web 管理界面，使用 React + Vite，并统一通过 `@workspace/ui`（shadcn/ui 风格组件）构建界面。

## 开发启动

```bash
pnpm install
pnpm --filter web dev
```

默认地址：`http://localhost:5173`  
`/api` 会自动代理到 `http://localhost:8080`。

## 生产构建

```bash
pnpm --filter web build
```

构建产物在 `apps/web/dist`。
