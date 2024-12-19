open Dagger.SDK

[<EntryPoint>]
let main args =
    async {
        let dag = Dagger.Connect()

        let! output =
            dag.Container().From("alpine").WithExec([| "echo"; "hello" |]).Stdout()
            |> Async.AwaitTask

        printfn $"{output}"
    }
    |> Async.RunSynchronously
    exit 0
