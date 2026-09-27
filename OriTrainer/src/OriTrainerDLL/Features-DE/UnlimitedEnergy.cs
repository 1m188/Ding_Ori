using System;

namespace OriTrainerDLL.Features
{
    // 无限能量：把当前能量保持为上限。
    //
    // ---- 为什么不用 System.Threading.Timer ----
    // 原版游戏（Unity 5.0 内置的旧 Mono 2.x）里，注入 DLL 用 System.Threading.Timer
    // 不可靠：计时器线程不被驱动，dueTime=0 的首次回调都不触发（实测：手动调一次
    // Refill 能把能量补满，但 Timer 自动回调永远不执行）。DE 版 Mono 正常、原版
    // 失效，同一份 DLL 行为不同，只能绕开 Timer。因此改挂游戏自己的每帧回调：
    //     Game.Events.Scheduler.OnGameFixedUpdate
    // 它在游戏主线程每个 FixedUpdate 调用，不依赖 mono 线程池，天然可靠。
    //
    // 频率足够：FixedUpdate 是 50Hz（20ms），写入与游戏帧循环同步，能量不会见底。
    // 主菜单/读档时 Sein 为 null，显式判空即可。
    public static class UnlimitedEnergy
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

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
                // 主菜单/读档过程中 Sein 为 null，显式判空
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                SeinEnergy energy = sein.Energy;
                if (energy == null) return;

                // 只在值不对时才写，避免每帧无谓写入
                if (energy.Current < energy.Max)
                    energy.Current = energy.Max;
            }
            catch { }
        }
    }
}
