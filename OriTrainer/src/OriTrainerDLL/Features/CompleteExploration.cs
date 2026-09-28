using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 100% 探索：hook GameWorld.CompletionAmount 恒返回 1f（即界面 100%），
    // 让 AchievementsLogic 每 5 秒采样读到 1.0 拿"完成地图"成就。
    //
    // ---- 原理 ----
    // 成就采样读的是顶层 GameWorld.Instance.CompletionAmount（GameWorld.cs）：
    //     float get_CompletionAmount() => 所有区域 CompletionAmount 的平均值；
    // hook 顶层 getter 恒返回 1f，采样直接读到 1.0，成立。
    //
    // ---- 为什么 hook 顶层而不是写区域字段（旧实现） ----
    // 旧实现在 OnGameFixedUpdate 里遍历所有 RuntimeGameWorldArea，反射写
    // m_completionAmount=1f + m_dirtyCompletionAmount=false。但 dirty 会被游戏
    // （移动/拾取）随时置回 true，get_CompletionAmount 看到 dirty 就调
    // UpdateCompletionAmount() 把我们写的 1.0 覆盖回真值，所以必须每帧压。
    // hook 顶层 getter 直接返回 1f，绕开整个缓存/dirty/重算链，无需每帧。
    //
    // ---- 停止 ----
    // Stop() 只还原 getter，CompletionAmount 恢复真实计算。本功能只在该位打开期间
    // 让完成度恒为 100%；关闭后恢复原样。
    public static class CompleteExploration
    {
        private static Hooks.Hook _hook;

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // 属性 getter：走 GetProperty+GetGetMethod 避免 specialname 坑（同 SoulFlameAnywhere）
            PropertyInfo prop = typeof(GameWorld).GetProperty("CompletionAmount",
                BindingFlags.Public | BindingFlags.Instance);
            MethodInfo target = (prop?.GetGetMethod(true)) ?? throw new Exception("GameWorld.CompletionAmount 与预期不符，功能无法工作");

            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(CompleteExploration).GetMethod("OnCompletionAmount",
                        BindingFlags.NonPublic | BindingFlags.Static));
        }

        public static void Stop()
        {
            if (_hook == null) return; // 幂等

            _hook.Dispose();
            _hook = null;
        }

        // 由游戏读取完成度时调用（代替 GameWorld.CompletionAmount 的 getter）。
        // 恒返回 1f = 100%。this 以第一参数传入。
        private static float OnCompletionAmount(GameWorld world)
        {
            return 1f;
        }
    }
}