using System;

namespace OriTrainerDLL.Features
{
    // 解锁全部基础技能：把暂停界面里显示的基础能力全部授予。
    //
    // 与原版/终极版的差异：终极版有 11 项基础能力（含 Grenade 光芒爆裂 与 Dash 冲刺），
    // 原版（Unity 5.0）的 PlayerAbilities 里【没有 Grenade 和 Dash】这两个字段
    // （反编译原版 Assembly-CSharp 确认），所以这里只写原版存在的 9 项基础能力：
    // Bash / ChargeFlame / WallJump / Stomp / DoubleJump / ChargeJump / Climb / Glide / SpiritFlame。
    // 其余（Magnet 等）是技能树被动，不属于"基础能力"，不授予（同终极版只给基础）。
    //
    // 实现同终极版，必须挂主线程钩子：能力组件要经
    // SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities() 用 Object.Instantiate
    // 实例化（Unity API，只能在主线程调），所以挂 Game.Events.Scheduler.OnGameFixedUpdate。
    // 持续写入是因为 RestartGame / 回标题再读档等操作会把标志位打回。
    //
    // 停止不还原：PlayerAbilities.Serialize() 会把 HasAbility 写进存档，开启后存过档
    // 能力就成了既有状态；刻意不还原也避免组件销毁后"关了再开就失效"。
    public static class UnlockAllAbilities
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 HasAbility：见文件头"停止不还原"
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                // 原版 9 项基础能力，全是 public 的 CharacterAbility 字段，直接赋值。
                playerAbilities.Bash.HasAbility = true;         // 猛击
                playerAbilities.ChargeFlame.HasAbility = true;  // 充能烈焰
                playerAbilities.WallJump.HasAbility = true;     // 飞檐走壁
                playerAbilities.Stomp.HasAbility = true;        // 践踏攻击
                playerAbilities.DoubleJump.HasAbility = true;   // 二段跳
                playerAbilities.ChargeJump.HasAbility = true;   // 充能跳跃
                playerAbilities.Climb.HasAbility = true;        // 攀爬
                playerAbilities.Glide.HasAbility = true;        // 黑子之羽
                playerAbilities.SpiritFlame.HasAbility = true;  // 精灵之火

                // 实例化对应的能力组件（本功能唯一需要主线程的一步）。
                // set_IsInstantiated 对同值会提前 return，每帧调用没有开销。
                SeinPrefabFactory prefabs = sein.Prefabs;
                prefabs?.EnsureRightPrefabsAreThereForAbilities();
            }
            catch { }
        }
    }
}
