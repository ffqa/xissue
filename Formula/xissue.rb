class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.10"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.10/xissue-v0.2.10-darwin-arm64.tar.gz"
      sha256 "36325492836c1e7dda749ca53e6191b57496fab81582a619b9d475328974bd59"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.10/xissue-v0.2.10-darwin-amd64.tar.gz"
      sha256 "2ded579b41186275325d1d9985e0e41b2293542e83262a80bc00b687d03befd4"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.10/xissue-v0.2.10-linux-arm64.tar.gz"
      sha256 "ff70b3a50303f4b7b0a54c225da905170f94e4c05a0d276012bf3f891db0fc34"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.10/xissue-v0.2.10-linux-amd64.tar.gz"
      sha256 "44f36a6fd73a7486516d173c53e32ea183f7f30711d12dcb2cf2986ad7a1db42"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
