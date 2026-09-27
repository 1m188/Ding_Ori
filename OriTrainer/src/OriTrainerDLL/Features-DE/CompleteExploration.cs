using System;
using System.Collections.Generic;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 100% 探索：把所有区域的完成度缓存直接写成 1.0（即界面 100%）。
    //
    // 本功能的目标是拿成就，不是真的去标记地图：
    //   AchievementsLogic.FixedUpdate 每 5 秒采样一次，判定
    //       Mathf.Approximately(GameWorld.Instance.CompletionAmount, 1.0f)
    //   成立就授予 CompleteMapAchievementAsset（"完成地图"）。
    //   这也是 get_CompletionPercentage 要先 Clamp(x*100, 0, 99) 再对 1.0 特判的原因
    //   —— 不到真正的 100%，界面最多显示 99。
    //
    // ---- 为什么必须同时清 m_dirtyCompletionAmount ----
    // m_completionAmount 只是缓存，真值由 UpdateCompletionAmount() 从底层状态算出：
    //     get_CompletionAmount:
    //         if (m_dirtyCompletionAmount) {
    //             m_dirtyCompletionAmount = false;
    //             UpdateCompletionAmount();     // ← 会把我们写的 1.0 覆盖回真值
    //         }
    //         return m_completionAmount;
    // 只写 1.0 不清 dirty 等于白写：游戏一读就算回真值。所以每周期两件事都要做。
    //
    // 游戏在移动 / 拾取时会调 VisitMapAreaAtPosition / DirtyCompletionAmount 把 dirty
    // 置回 true，所以必须持续压制（golang 版同样每周期重写）。
    //
    // ---- 为什么用反射 ----
    // m_completionAmount 与 m_dirtyCompletionAmount 都是 private 字段，游戏没有暴露
    // 可写的公开入口（VisitAllAreas() 只改 m_worldAreaStates，不含图标那部分，
    // 因此达不到 100%，见文件末说明）。golang 版是裸内存写
    // +0x14 / +0x18，这里用元数据取同样的效果，不必自己算偏移。
    //
    // ---- 为什么挂主线程钩子 ----
    // 这条链上没有任何 Unity native 调用：GameWorld.Instance 判空走
    // UnityEngine.Object::op_Equality → CompareBaseObjects → IsNativeObjectAlive →
    // GetCachedPtr，全是托管 IL；遍历 RuntimeAreas（普通 List<T>）与写字段同理。
    // 本来用定时器即可。但原版游戏（Unity 5.0 内置的旧 Mono 2.x）里注入 DLL 的
    // System.Threading.Timer 不可靠（回调不触发，实测），所以统一改挂游戏自己的
    // 每帧回调 OnGameFixedUpdate，与 InfiniteDash / ShowMap 同模式。
    //
    // ---- 遍历不用手工解引用 ----
    // 这里 GameWorld.RuntimeAreas 是 public 的 List<RuntimeGameWorldArea>，直接 foreach。
    //
    // ---- 停止不还原 ----
    // Stop() 只注销钩子。m_completionAmount 是纯运行时缓存，不进存档
    // （RuntimeGameWorldArea.Serialize 只写 m_worldAreaStates 与 Icons），
    // 重启游戏后界面自然回到真实完成度；而成就一经授予即永久保留。
    // 因此还原没有意义 —— 本功能的作用是"让采样那一刻读到 1.0"。
    //
    // ---- 为什么不走官方 API ----
    // 游戏提供了 public 的 VisitAllAreas()（游戏自己的调试菜单 DebugMenuB.VisitAllAreas
    // 就是调它 + UpdateCompletionAmount）。但完成度公式是
    //     (visitedFaces + totalIcons - missingIcons) / (totalFaces + totalIcons)
    // VisitAllAreas() 只把地形面置为 Visited，不碰图标（血/能量容器、存档点等），
    // 所以只能到"地形全走遍"的百分比，达不到 1.0，拿不到成就。
    //
    // ---- 时机 ----
    // 采样是每 5 秒一次。移动中游戏会反复把 dirty 置回 true，若恰好撞上采样则当次落空，
    // 但它每 5 秒重试；站住不动几秒必然成功（不动就不会再置 dirty）。
    // 成就闸门是 CheatsHandler.DebugWasEnabled（开过官方作弊菜单才会被拦下）。
    public static class CompleteExploration
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fAmount; // m_completionAmount (Single, 0..1)
        private static FieldInfo _fDirty;  // m_dirtyCompletionAmount (Boolean)
        private static Action _hook;       // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            Type t = typeof(RuntimeGameWorldArea);
            _fAmount = t.GetField("m_completionAmount", Private);
            _fDirty = t.GetField("m_dirtyCompletionAmount", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            if (_fAmount == null || _fDirty == null)
                throw new Exception("RuntimeGameWorldArea 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                // 未进游戏、或对象已销毁时为 null。GameWorld 是 MonoBehaviour，
                // 这里的 != null 走 UnityEngine.Object::op_Equality，假空同样会被判为 null。
                GameWorld world = GameWorld.Instance;
                if (world == null) return;

                List<RuntimeGameWorldArea> areas = world.RuntimeAreas;
                if (areas == null) return;

                for (int i = 0; i < areas.Count; i++)
                {
                    RuntimeGameWorldArea area = areas[i];
                    if (area == null) continue;

                    _fAmount.SetValue(area, 1f);
                    _fDirty.SetValue(area, false); // 不清就会被 UpdateCompletionAmount 覆盖
                }
            }
            catch { }
        }
    }
}
